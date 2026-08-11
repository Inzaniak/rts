package fsx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/google/uuid"

	"github.com/Inzaniak/rts/internal/core"
)

type Executor struct {
	BackupRoot string
	DryRun     bool
}

type backupManifest struct {
	ID        string        `json:"id"`
	Summary   string        `json:"summary"`
	CreatedAt time.Time     `json:"createdAt"`
	Entries   []backupEntry `json:"entries"`
}

type backupEntry struct {
	Path       string      `json:"path"`
	BackupPath string      `json:"backupPath,omitempty"`
	Existed    bool        `json:"existed"`
	Mode       os.FileMode `json:"mode,omitempty"`
}

func (e *Executor) Apply(ctx context.Context, change core.ChangeSet) (core.ApplyResult, error) {
	if len(change.Operations) == 0 {
		return core.ApplyResult{}, errors.New("changeset has no operations")
	}
	if change.ID == "" {
		change.ID = uuid.NewString()
	}
	if e.DryRun {
		paths := changedPaths(change.Operations)
		return core.ApplyResult{TransactionID: change.ID, Changed: paths}, nil
	}

	unlock, err := lockPaths(change.Operations)
	if err != nil {
		return core.ApplyResult{}, err
	}
	defer unlock()

	if err := checkPreconditions(change.Operations); err != nil {
		return core.ApplyResult{}, err
	}
	backupDir := filepath.Join(e.BackupRoot, time.Now().UTC().Format("20060102T150405Z")+"-"+change.ID)
	manifest, err := createBackups(backupDir, change)
	if err != nil {
		return core.ApplyResult{}, err
	}

	var applied []core.Operation
	for _, op := range change.Operations {
		if err := applyOperation(ctx, op); err != nil {
			rollbackErr := restoreManifest(manifest)
			if rollbackErr != nil {
				return core.ApplyResult{}, fmt.Errorf("apply %q: %w; rollback also failed: %v", op.Description, err, rollbackErr)
			}
			return core.ApplyResult{}, fmt.Errorf("apply %q: %w; file changes rolled back", op.Description, err)
		}
		applied = append(applied, op)
	}
	_ = applied
	if err := writeManifest(backupDir, manifest); err != nil {
		return core.ApplyResult{}, fmt.Errorf("write transaction manifest: %w", err)
	}
	return core.ApplyResult{
		TransactionID: change.ID,
		Changed:       changedPaths(change.Operations),
		BackupDir:     backupDir,
	}, nil
}

func (e *Executor) Restore(ctx context.Context, backupDir string) (core.ApplyResult, error) {
	manifestPath := filepath.Join(backupDir, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return core.ApplyResult{}, err
	}
	var manifest backupManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return core.ApplyResult{}, fmt.Errorf("parse backup manifest: %w", err)
	}
	_ = ctx
	if err := restoreManifest(manifest); err != nil {
		return core.ApplyResult{}, err
	}
	var changed []string
	for _, entry := range manifest.Entries {
		changed = append(changed, entry.Path)
	}
	return core.ApplyResult{TransactionID: manifest.ID, Changed: changed, BackupDir: backupDir}, nil
}

func changedPaths(ops []core.Operation) []string {
	seen := map[string]bool{}
	var result []string
	for _, op := range ops {
		if op.Source != "" && !seen[op.Source] {
			seen[op.Source] = true
			result = append(result, op.Source)
		}
		if op.Path != "" && !seen[op.Path] {
			seen[op.Path] = true
			result = append(result, op.Path)
		}
	}
	sort.Strings(result)
	return result
}

func lockPaths(ops []core.Operation) (func(), error) {
	paths := changedPaths(ops)
	var locks []*flock.Flock
	for _, path := range paths {
		lockPath := path + ".rts.lock"
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
			unlockAll(locks)
			return nil, err
		}
		f := flock.New(lockPath)
		ok, err := f.TryLock()
		if err != nil || !ok {
			unlockAll(locks)
			if err != nil {
				return nil, fmt.Errorf("lock %s: %w", path, err)
			}
			return nil, fmt.Errorf("resource is being modified by another process: %s", path)
		}
		locks = append(locks, f)
	}
	return func() {
		unlockAll(locks)
		for _, path := range paths {
			_ = os.Remove(path + ".rts.lock")
		}
	}, nil
}

func unlockAll(locks []*flock.Flock) {
	for i := len(locks) - 1; i >= 0; i-- {
		_ = locks[i].Unlock()
	}
}

func checkPreconditions(ops []core.Operation) error {
	for _, op := range ops {
		if op.ExpectedHash == "" {
			continue
		}
		path := op.Path
		if op.Type == core.OpMove {
			path = op.Source
		}
		hash, err := core.HashPath(path)
		if err != nil {
			return fmt.Errorf("precondition %s: %w", path, err)
		}
		if hash != op.ExpectedHash {
			return fmt.Errorf("stale resource %s: expected %s, found %s", path, shortHash(op.ExpectedHash), shortHash(hash))
		}
	}
	return nil
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func createBackups(backupDir string, change core.ChangeSet) (backupManifest, error) {
	manifest := backupManifest{ID: change.ID, Summary: change.Summary, CreatedAt: time.Now().UTC()}
	for index, path := range changedPaths(change.Operations) {
		entry := backupEntry{Path: path}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			manifest.Entries = append(manifest.Entries, entry)
			continue
		}
		if err != nil {
			return manifest, fmt.Errorf("inspect %s: %w", path, err)
		}
		entry.Existed = true
		entry.Mode = info.Mode()
		entry.BackupPath = filepath.Join(backupDir, fmt.Sprintf("%03d", index))
		if err := copyPath(path, entry.BackupPath); err != nil {
			return manifest, fmt.Errorf("backup %s: %w", path, err)
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	return manifest, nil
}

func applyOperation(ctx context.Context, op core.Operation) error {
	switch op.Type {
	case core.OpMkdir:
		mode := op.Mode
		if mode == 0 {
			mode = 0o755
		}
		return os.MkdirAll(op.Path, mode)
	case core.OpWrite:
		mode := op.Mode
		if mode == 0 {
			mode = 0o644
			if info, err := os.Stat(op.Path); err == nil {
				mode = info.Mode().Perm()
			}
		}
		if err := os.MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(op.Path), ".rts-write-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if err := tmp.Chmod(mode); err != nil {
			tmp.Close()
			return err
		}
		if _, err := tmp.Write(op.Content); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmpName, op.Path)
	case core.OpRemove:
		info, err := os.Lstat(op.Path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.RemoveAll(op.Path)
		}
		return os.Remove(op.Path)
	case core.OpMove:
		if op.Source == "" || op.Path == "" {
			return errors.New("move operation requires source and destination")
		}
		if _, err := os.Lstat(op.Path); err == nil {
			return fmt.Errorf("move destination already exists: %s", op.Path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(op.Path), 0o700); err != nil {
			return err
		}
		if err := os.Rename(op.Source, op.Path); err == nil {
			return nil
		} else if !errors.Is(err, syscall.EXDEV) {
			return err
		}
		if err := copyPath(op.Source, op.Path); err != nil {
			return err
		}
		info, err := os.Lstat(op.Source)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.RemoveAll(op.Source)
		}
		return os.Remove(op.Source)
	case core.OpCommand:
		cmd := exec.CommandContext(ctx, op.Command, op.Args...)
		cmd.Dir = op.Dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			return errors.New(message)
		}
		return nil
	default:
		return fmt.Errorf("unsupported operation %q", op.Type)
	}
}

func writeManifest(dir string, manifest backupManifest) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o600)
}

func restoreManifest(manifest backupManifest) error {
	for i := len(manifest.Entries) - 1; i >= 0; i-- {
		entry := manifest.Entries[i]
		if !entry.Existed {
			if err := os.RemoveAll(entry.Path); err != nil {
				return err
			}
			continue
		}
		if err := os.RemoveAll(entry.Path); err != nil {
			return err
		}
		if err := copyPath(entry.BackupPath, entry.Path); err != nil {
			return err
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
