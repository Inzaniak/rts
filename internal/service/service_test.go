package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Inzaniak/rts/internal/adapters"
	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/store"
)

func TestExplicitSkillSyncDetectsAndResolvesDrift(t *testing.T) {
	project := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(configRoot, "claude"))
	t.Setenv("CODEX_HOME", filepath.Join(configRoot, "codex"))

	sourcePath := filepath.Join(project, ".claude", "skills", "review")
	targetPath := filepath.Join(project, ".agents", "skills", "review")
	for _, path := range []string{sourcePath, targetPath} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	initial := []byte("---\nname: review\ndescription: Review code.\n---\n\nOld instructions.\n")
	os.WriteFile(filepath.Join(sourcePath, "SKILL.md"), initial, 0o644)
	os.WriteFile(filepath.Join(targetPath, "SKILL.md"), initial, 0o644)

	state, err := store.Open(filepath.Join(configRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	svc := New(adapters.All(), state, configRoot)
	resources, err := svc.Inventory(context.Background(), project, Filters{Kind: core.KindSkill})
	if err != nil {
		t.Fatal(err)
	}
	var source, target core.Resource
	for _, resource := range resources {
		if resource.Harness == core.Claude && resource.Path == sourcePath {
			source = resource
		}
		if resource.Harness == core.Codex && resource.Path == targetPath {
			target = resource
		}
	}
	if source.ID == "" || target.ID == "" {
		t.Fatalf("expected source and target resources, found %#v", resources)
	}
	link, err := svc.Link(context.Background(), project, source.ID, []string{target.ID})
	if err != nil {
		t.Fatal(err)
	}
	updated := []byte("---\nname: review\ndescription: Review code.\n---\n\nNew instructions.\n")
	os.WriteFile(filepath.Join(sourcePath, "SKILL.md"), updated, 0o644)
	drift, err := svc.Drift(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0].Status[drift[0].Source.Key()] != "changed" {
		t.Fatalf("source drift was not detected: %#v", drift)
	}
	if _, _, err := svc.Sync(context.Background(), project, link.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(targetPath, "SKILL.md"))
	if string(got) != string(updated) {
		t.Fatalf("target was not synchronized:\n%s", got)
	}
	drift, err = svc.Drift(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range drift[0].Status {
		if status != "clean" {
			t.Fatalf("expected clean link after sync, got %#v", drift[0].Status)
		}
	}
}

func TestTranslateMCPBetweenOpenCodeAndAntigravity(t *testing.T) {
	source := core.Resource{Harness: core.OpenCode, Kind: core.KindMCP, Name: "docs"}
	target := core.Resource{Harness: core.Antigravity, Kind: core.KindMCP, Name: "docs"}
	content := []byte(`{"type":"remote","url":"https://example.com/mcp","enabled":true}`)
	translated, err := translatePortable(source, target, content)
	if err != nil {
		t.Fatal(err)
	}
	text := string(translated)
	if !strings.Contains(text, `"serverUrl": "https://example.com/mcp"`) {
		t.Fatalf("Antigravity serverUrl was not produced:\n%s", text)
	}
	if strings.Contains(text, `"enabled"`) || strings.Contains(text, `"type"`) {
		t.Fatalf("OpenCode-only fields leaked into translation:\n%s", text)
	}
}

func TestDisableAndEnableSkillDirectoryWithCollisionProtection(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "claude")
	configRoot := filepath.Join(root, "rts")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	skillPath := filepath.Join(claudeHome, "skills", "review")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\ndescription: Review code.\n---\n\nInstructions.\n")
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), content, 0o640); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(configRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	svc := New(adapters.All(), state, configRoot)

	resource := findInventoryResource(t, svc, "", core.Claude, core.KindSkill, "review")
	if resource.Enabled == nil || !*resource.Enabled || !resource.Has(core.CanEnable) {
		t.Fatalf("active resource is not toggleable: %#v", resource)
	}
	change, _, err := svc.Toggle(context.Background(), resource, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Operations) != 2 || change.Operations[1].Type != core.OpMove {
		t.Fatalf("dry-run plan does not store the resource: %#v", change)
	}
	if _, err := os.Stat(filepath.Join(skillPath, "SKILL.md")); err != nil {
		t.Fatalf("dry-run moved the native skill: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(configRoot, "disabled", "manifests", "*.json")); len(matches) != 0 {
		t.Fatalf("dry-run created disabled state: %#v", matches)
	}
	if _, _, err := svc.Toggle(context.Background(), resource, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(skillPath); !os.IsNotExist(err) {
		t.Fatalf("native skill still exists: %v", err)
	}
	disabled := findInventoryResource(t, svc, "", core.Claude, core.KindSkill, "review")
	if disabled.Enabled == nil || *disabled.Enabled || !isDisabledResource(disabled) {
		t.Fatalf("resource is not represented as disabled: %#v", disabled)
	}
	if disabled.ID != resource.ID || disabled.Path != resource.Path {
		t.Fatalf("disabled identity changed: before=%#v after=%#v", resource, disabled)
	}
	if got, err := svc.Read(disabled); err != nil || string(got) != string(content) {
		t.Fatalf("stored content = %q, %v", got, err)
	}

	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("collision\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Toggle(context.Background(), disabled, true, false); err == nil {
		t.Fatal("expected enable collision error")
	}
	if got, _ := os.ReadFile(filepath.Join(skillPath, "SKILL.md")); string(got) != "collision\n" {
		t.Fatal("collision content was overwritten")
	}
	if err := os.RemoveAll(skillPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Toggle(context.Background(), disabled, true, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(skillPath, "SKILL.md")); string(got) != string(content) {
		t.Fatalf("restored content = %q", got)
	}
	info, _ := os.Stat(filepath.Join(skillPath, "SKILL.md"))
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored mode = %o", info.Mode().Perm())
	}
}

func TestDisableAndEnableSymlinkedSkill(t *testing.T) {
	root := t.TempDir()
	claudeHome := filepath.Join(root, "claude")
	configRoot := filepath.Join(root, "rts")
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	target := filepath.Join(root, "skill-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\ndescription: Linked.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(claudeHome, "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, skillPath); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(configRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	svc := New(adapters.All(), state, configRoot)

	resource := findInventoryResource(t, svc, "", core.Claude, core.KindSkill, "linked")
	if _, _, err := svc.Toggle(context.Background(), resource, false, false); err != nil {
		t.Fatal(err)
	}
	disabled := findInventoryResource(t, svc, "", core.Claude, core.KindSkill, "linked")
	if _, _, err := svc.Toggle(context.Background(), disabled, true, false); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(skillPath); err != nil || got != target {
		t.Fatalf("restored symlink = %q, %v", got, err)
	}
}

func TestDisableAndEnableEmbeddedMCPEntry(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex")
	configRoot := filepath.Join(root, "rts")
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(`model = "keep-me"

[mcp_servers.docs]
url = "https://example.com/mcp"
enabled = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(configRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	svc := New(adapters.All(), state, configRoot)

	resource := findInventoryResource(t, svc, "", core.Codex, core.KindMCP, "docs")
	if _, _, err := svc.Toggle(context.Background(), resource, false, false); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), "mcp_servers.docs") || !strings.Contains(string(raw), `model = "keep-me"`) {
		t.Fatalf("MCP disable changed the wrong content:\n%s", raw)
	}
	disabled := findInventoryResource(t, svc, "", core.Codex, core.KindMCP, "docs")
	if got, err := svc.Read(disabled); err != nil || !strings.Contains(string(got), "https://example.com/mcp") {
		t.Fatalf("stored MCP content = %q, %v", got, err)
	}
	if err := os.WriteFile(configPath, []byte(`model = "keep-me"

[mcp_servers.docs]
url = "https://collision.example/mcp"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	occupied := findInventoryResource(t, svc, "", core.Codex, core.KindMCP, "docs")
	if !isDisabledResource(occupied) || !strings.Contains(strings.Join(occupied.Warnings, " "), "occupied") {
		t.Fatalf("disabled collision was not authoritative: %#v", occupied)
	}
	if _, _, err := svc.Toggle(context.Background(), occupied, true, false); err == nil {
		t.Fatal("expected embedded-entry collision error")
	}
	raw, _ = os.ReadFile(configPath)
	if !strings.Contains(string(raw), "https://collision.example/mcp") {
		t.Fatal("embedded collision was overwritten")
	}
	if err := os.WriteFile(configPath, []byte(`model = "keep-me"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Toggle(context.Background(), disabled, true, false); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(configPath)
	if !strings.Contains(string(raw), "https://example.com/mcp") || !strings.Contains(string(raw), `model = "keep-me"`) {
		t.Fatalf("MCP restore failed:\n%s", raw)
	}
}

func findInventoryResource(
	t *testing.T,
	svc *Service,
	project string,
	harness core.Harness,
	kind core.Kind,
	name string,
) core.Resource {
	t.Helper()
	resources, err := svc.Inventory(context.Background(), project, Filters{Harness: harness, Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Name == name {
			return resource
		}
	}
	t.Fatalf("resource %s/%s/%s was not found in %#v", harness, kind, name, resources)
	return core.Resource{}
}
