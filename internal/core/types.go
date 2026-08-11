package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Harness string

const (
	Claude      Harness = "claude"
	Codex       Harness = "codex"
	Grok        Harness = "grok"
	Antigravity Harness = "antigravity"
	OpenCode    Harness = "opencode"
	Copilot     Harness = "copilot"
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeLocal   Scope = "local"
	ScopeManaged Scope = "managed"
	ScopePlugin  Scope = "plugin"
)

type Kind string

const (
	KindSettings     Kind = "settings"
	KindInstructions Kind = "instructions"
	KindRule         Kind = "rule"
	KindSkill        Kind = "skill"
	KindCommand      Kind = "command"
	KindAgent        Kind = "agent"
	KindWorkflow     Kind = "workflow"
	KindMCP          Kind = "mcp"
	KindHook         Kind = "hook"
	KindPlugin       Kind = "plugin"
	KindMarketplace  Kind = "marketplace"
	KindLSP          Kind = "lsp"
	KindTool         Kind = "tool"
	KindTheme        Kind = "theme"
	KindOutputStyle  Kind = "output-style"
	KindKeybindings  Kind = "keybindings"
	KindProfile      Kind = "profile"
	KindPermissions  Kind = "permissions"
	KindMemory       Kind = "memory"
	KindExtension    Kind = "extension"
	KindWorktree     Kind = "worktree"
)

var Kinds = []Kind{
	KindSettings, KindInstructions, KindRule, KindSkill, KindCommand, KindAgent,
	KindWorkflow, KindMCP, KindHook, KindPlugin, KindMarketplace, KindLSP,
	KindTool, KindTheme, KindOutputStyle, KindKeybindings, KindProfile,
	KindPermissions, KindMemory, KindExtension, KindWorktree,
}

type Capability string

const (
	CanRead    Capability = "read"
	CanCreate  Capability = "create"
	CanUpdate  Capability = "update"
	CanDelete  Capability = "delete"
	CanEnable  Capability = "enable"
	CanInstall Capability = "install"
)

type Resource struct {
	ID           string         `json:"id"`
	Harness      Harness        `json:"harness"`
	Surface      string         `json:"surface,omitempty"`
	Version      string         `json:"version,omitempty"`
	Kind         Kind           `json:"kind"`
	Scope        Scope          `json:"scope"`
	ProjectRoot  string         `json:"projectRoot,omitempty"`
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	Locator      string         `json:"locator,omitempty"`
	Format       string         `json:"format,omitempty"`
	Origin       string         `json:"origin,omitempty"`
	Fingerprint  string         `json:"fingerprint,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	ReadOnly     bool           `json:"readOnly,omitempty"`
	Capabilities []Capability   `json:"capabilities"`
	Warnings     []string       `json:"warnings,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

func (r Resource) Key() string {
	return strings.Join([]string{string(r.Harness), string(r.Scope), string(r.Kind), r.Name, r.Path, r.Locator}, "\x00")
}

func (r Resource) Has(capability Capability) bool {
	for _, c := range r.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

type Installation struct {
	Harness    Harness  `json:"harness"`
	Surface    string   `json:"surface"`
	Version    string   `json:"version,omitempty"`
	Executable string   `json:"executable,omitempty"`
	ConfigHome string   `json:"configHome,omitempty"`
	Detected   bool     `json:"detected"`
	Warnings   []string `json:"warnings,omitempty"`
}

type Diagnostic struct {
	Severity string  `json:"severity"`
	Harness  Harness `json:"harness,omitempty"`
	Resource string  `json:"resource,omitempty"`
	Message  string  `json:"message"`
	Hint     string  `json:"hint,omitempty"`
}

type Request struct {
	Harness Harness
	Kind    Kind
	Scope   Scope
	Name    string
	Project string
	Content []byte
	Force   bool
}

type OperationType string

const (
	OpWrite   OperationType = "write"
	OpRemove  OperationType = "remove"
	OpMove    OperationType = "move"
	OpCommand OperationType = "command"
)

type Operation struct {
	Type         OperationType `json:"type"`
	Source       string        `json:"source,omitempty"`
	Path         string        `json:"path,omitempty"`
	Content      []byte        `json:"-"`
	ExpectedHash string        `json:"expectedHash,omitempty"`
	Mode         os.FileMode   `json:"mode,omitempty"`
	Command      string        `json:"command,omitempty"`
	Args         []string      `json:"args,omitempty"`
	Description  string        `json:"description"`
}

type ChangeSet struct {
	ID         string      `json:"id"`
	Summary    string      `json:"summary"`
	Operations []Operation `json:"operations"`
	Warnings   []string    `json:"warnings,omitempty"`
	CreatedAt  time.Time   `json:"createdAt"`
}

type ApplyResult struct {
	TransactionID string   `json:"transactionId"`
	Changed       []string `json:"changed"`
	BackupDir     string   `json:"backupDir,omitempty"`
}

type Envelope struct {
	SchemaVersion string         `json:"schemaVersion"`
	Data          any            `json:"data,omitempty"`
	Warnings      []string       `json:"warnings,omitempty"`
	Errors        []string       `json:"errors,omitempty"`
	Meta          map[string]any `json:"meta,omitempty"`
}

func NewEnvelope(data any) Envelope {
	return Envelope{SchemaVersion: "rts.v1", Data: data}
}

func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func HashPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return HashFile(path)
	}
	var paths []string
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to hash symlink %s", p)
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		rel, _ := filepath.Rel(path, p)
		h.Write([]byte(filepath.ToSlash(rel)))
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			return "", readErr
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ResourceID(r Resource) string {
	return HashBytes([]byte(r.Key()))[:20]
}

func ParseKind(value string) (Kind, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	for _, kind := range Kinds {
		if string(kind) == value {
			return kind, nil
		}
	}
	return "", fmt.Errorf("unknown resource kind %q", value)
}

func ParseHarness(value string) (Harness, error) {
	switch Harness(strings.ToLower(strings.TrimSpace(value))) {
	case Claude:
		return Claude, nil
	case Codex:
		return Codex, nil
	case Grok:
		return Grok, nil
	case Antigravity:
		return Antigravity, nil
	case OpenCode:
		return OpenCode, nil
	case Copilot:
		return Copilot, nil
	default:
		return "", fmt.Errorf("unknown harness %q", value)
	}
}

func ParseScope(value string) (Scope, error) {
	switch Scope(strings.ToLower(strings.TrimSpace(value))) {
	case ScopeUser:
		return ScopeUser, nil
	case ScopeProject:
		return ScopeProject, nil
	case ScopeLocal:
		return ScopeLocal, nil
	case ScopeManaged:
		return ScopeManaged, nil
	case ScopePlugin:
		return ScopePlugin, nil
	default:
		return "", fmt.Errorf("unknown scope %q", value)
	}
}

func PrettyJSON(value any) []byte {
	b, _ := json.MarshalIndent(value, "", "  ")
	return append(b, '\n')
}
