package fsx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Inzaniak/rts/internal/core"
)

func TestApplyBackupAndRestore(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "config.json")
	if err := os.WriteFile(target, []byte("before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	hash, _ := core.HashFile(target)
	executor := &Executor{BackupRoot: filepath.Join(root, "backups")}
	change := core.ChangeSet{
		ID: "tx-one", Summary: "update", CreatedAt: time.Now(),
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: target, Content: []byte("after\n"),
			ExpectedHash: hash, Description: "update config",
		}},
	}
	result, err := executor.Apply(context.Background(), change)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "after\n" {
		t.Fatalf("unexpected applied content %q", got)
	}
	if _, err := executor.Restore(context.Background(), result.BackupDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "before\n" {
		t.Fatalf("unexpected restored content %q", got)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed to %o", info.Mode().Perm())
	}
}

func TestApplyRejectsStaleHash(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	os.WriteFile(target, []byte("{}\n"), 0o644)
	executor := &Executor{BackupRoot: filepath.Join(root, "backups")}
	_, err := executor.Apply(context.Background(), core.ChangeSet{
		ID: "stale", Summary: "stale",
		Operations: []core.Operation{{
			Type: core.OpWrite, Path: target, Content: []byte(`{"x":1}`),
			ExpectedHash: "not-the-current-hash", Description: "stale update",
		}},
	})
	if err == nil {
		t.Fatal("expected stale-hash error")
	}
	if got, _ := os.ReadFile(target); string(got) != "{}\n" {
		t.Fatal("stale mutation changed the file")
	}
}

func TestApplyRollsBackWhenLaterOperationFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings.json")
	os.WriteFile(target, []byte("before\n"), 0o644)
	hash, _ := core.HashFile(target)
	executor := &Executor{BackupRoot: filepath.Join(root, "backups")}
	_, err := executor.Apply(context.Background(), core.ChangeSet{
		ID: "rollback", Summary: "rollback",
		Operations: []core.Operation{
			{Type: core.OpWrite, Path: target, Content: []byte("after\n"), ExpectedHash: hash, Description: "write"},
			{Type: core.OpCommand, Command: "definitely-not-an-rts-test-command", Description: "fail"},
		},
	})
	if err == nil {
		t.Fatal("expected command failure")
	}
	if got, _ := os.ReadFile(target); string(got) != "before\n" {
		t.Fatalf("rollback failed, got %q", got)
	}
}

func TestMoveAndRestoreSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	source := filepath.Join(root, "linked")
	stored := filepath.Join(root, "disabled", "linked")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{BackupRoot: filepath.Join(root, "backups")}
	result, err := executor.Apply(context.Background(), core.ChangeSet{
		ID: "move-link", Summary: "move symlink",
		Operations: []core.Operation{{
			Type: core.OpMove, Source: source, Path: stored, Description: "store symlink",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists: %v", err)
	}
	if got, err := os.Readlink(stored); err != nil || got != target {
		t.Fatalf("stored symlink = %q, %v", got, err)
	}
	if _, err := executor.Restore(context.Background(), result.BackupDir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(source); err != nil || got != target {
		t.Fatalf("restored symlink = %q, %v", got, err)
	}
	if _, err := os.Lstat(stored); !os.IsNotExist(err) {
		t.Fatalf("stored path survived restore: %v", err)
	}
}

func TestMoveRollsBackWhenLaterOperationFails(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "native", "resource")
	stored := filepath.Join(root, "disabled", "resource")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "content"), []byte("keep\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{BackupRoot: filepath.Join(root, "backups")}
	_, err := executor.Apply(context.Background(), core.ChangeSet{
		ID: "move-rollback", Summary: "move then fail",
		Operations: []core.Operation{
			{Type: core.OpMove, Source: source, Path: stored, Description: "move"},
			{Type: core.OpCommand, Command: "definitely-not-an-rts-test-command", Description: "fail"},
		},
	})
	if err == nil {
		t.Fatal("expected command failure")
	}
	if got, readErr := os.ReadFile(filepath.Join(source, "content")); readErr != nil || string(got) != "keep\n" {
		t.Fatalf("source was not rolled back: %q, %v", got, readErr)
	}
	if _, statErr := os.Lstat(stored); !os.IsNotExist(statErr) {
		t.Fatalf("stored path survived rollback: %v", statErr)
	}
}
