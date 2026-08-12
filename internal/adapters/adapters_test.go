package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Inzaniak/rts/internal/core"
)

func TestClaudeProjectDiscoveryAndMCPMutation(t *testing.T) {
	project := t.TempDir()
	skill := filepath.Join(project, ".claude", "skills", "review")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\ndescription: Review changes.\n---\n"), 0o644)
	mcpPath := filepath.Join(project, ".mcp.json")
	os.WriteFile(mcpPath, []byte("{\n  // retain me\n  \"mcpServers\": {}\n}\n"), 0o644)

	driver := claude()
	resources, err := driver.Discover(project)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(resources, core.KindSkill, "review") {
		t.Fatal("Claude project skill was not discovered")
	}
	change, err := driver.PlanCreate(core.Request{
		Harness: core.Claude, Kind: core.KindMCP, Scope: core.ScopeProject,
		Name: "docs", Project: project, Content: []byte(`{"url":"https://example.com/mcp"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Operations) != 1 || !strings.Contains(string(change.Operations[0].Content), "// retain me") {
		t.Fatal("MCP plan did not preserve the JSONC comment")
	}
}

func TestCodexTOMLMCPDiscoveryKeepsNativePayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	raw := []byte(`# keep
model = "gpt-test"

[mcp_servers.docs]
url = "https://example.com/mcp"
enabled = true
`)
	os.WriteFile(filepath.Join(home, "config.toml"), raw, 0o644)
	driver := codex()
	resources, err := driver.Discover("")
	if err != nil {
		t.Fatal(err)
	}
	var mcp *core.Resource
	for index := range resources {
		if resources[index].Kind == core.KindMCP && resources[index].Name == "docs" {
			mcp = &resources[index]
		}
	}
	if mcp == nil {
		t.Fatal("Codex MCP server was not discovered")
	}
	config := mcp.Metadata["config"].(map[string]any)
	if config["url"] != "https://example.com/mcp" {
		t.Fatalf("native MCP payload missing: %#v", config)
	}
	change, err := driver.PlanUpdate(*mcp, []byte(`{"url":"https://new.example/mcp","enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(change.Operations[0].Content), "# keep") ||
		!strings.Contains(string(change.Operations[0].Content), `model = "gpt-test"`) {
		t.Fatal("Codex update did not preserve unrelated TOML")
	}
}

func TestCodexDiscoversUserSystemAndEnabledPluginSkills(t *testing.T) {
	userHome := t.TempDir()
	codexHome := filepath.Join(userHome, ".codex")
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("CODEX_HOME", codexHome)

	writeSkill := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\ndescription: Test skill.\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(filepath.Join(userHome, ".agents", "skills", "shared"))
	writeSkill(filepath.Join(codexHome, "skills", "personal"))
	writeSkill(filepath.Join(codexHome, "skills", "nested-package", "skills", "nested"))
	writeSkill(filepath.Join(codexHome, "skills", ".system", "system"))
	linkedSkill := filepath.Join(userHome, "linked-skill")
	writeSkill(linkedSkill)
	if err := os.Symlink(linkedSkill, filepath.Join(codexHome, "skills", "linked")); err != nil {
		t.Fatal(err)
	}
	writeSkill(filepath.Join(codexHome, "plugins", "cache", "market-remote", "example", "1.0.0", "skills", "plugin-skill"))
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(`
[plugins."example@market"]
enabled = true

[plugins."disabled@market"]
enabled = false
`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(codexHome, "plugins", "cache", "market-remote", "example", "1.0.0", ".codex-plugin")
	if err := os.MkdirAll(manifest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifest, "plugin.json"), []byte(`{"name":"example"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := codex().Discover("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shared", "personal", "nested", "linked", "system", "example:plugin-skill"} {
		if !hasResource(resources, core.KindSkill, name) {
			t.Errorf("Codex skill %q was not discovered", name)
		}
	}
	system := findResource(resources, core.KindSkill, "system")
	if system == nil || system.Scope != core.ScopeManaged || !system.ReadOnly {
		t.Errorf("system skill should be managed and read-only: %#v", system)
	}
	plugin := findResource(resources, core.KindSkill, "example:plugin-skill")
	if plugin == nil || plugin.Scope != core.ScopePlugin || !plugin.ReadOnly || plugin.Origin != "example@market" {
		t.Errorf("plugin skill metadata is incorrect: %#v", plugin)
	}
}

func TestGrokDiscoveryAndTOMLMCPMutation(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("GROK_HOME", home)

	userSkill := filepath.Join(home, "skills", "review")
	projectAgent := filepath.Join(project, ".grok", "agents")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectAgent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("---\ndescription: Review changes.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectAgent, "reviewer.md"), []byte("---\nname: reviewer\n---\nReview changes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(project, ".grok", "config.toml")
	if err := os.WriteFile(config, []byte(`# keep
[permission]
allow = ["Bash(go test *)"]

[mcp_servers.docs]
url = "https://example.com/mcp"
enabled = true
`), 0o644); err != nil {
		t.Fatal(err)
	}

	driver := grok()
	resources, err := driver.Discover(project)
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(resources, core.KindSkill, "review") {
		t.Fatal("Grok user skill was not discovered")
	}
	if !hasResource(resources, core.KindAgent, "reviewer") {
		t.Fatal("Grok project agent was not discovered")
	}
	mcp := findResource(resources, core.KindMCP, "docs")
	if mcp == nil || mcp.Scope != core.ScopeProject {
		t.Fatalf("Grok project MCP server was not discovered: %#v", mcp)
	}
	change, err := driver.PlanUpdate(*mcp, []byte(`{"url":"https://new.example/mcp","enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	updated := string(change.Operations[0].Content)
	if !strings.Contains(updated, "# keep") || !strings.Contains(updated, `allow = ["Bash(go test *)"]`) {
		t.Fatalf("Grok MCP update did not preserve unrelated TOML:\n%s", updated)
	}
}

func TestGrokManagedPoliciesAreReadOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "requirements.toml"), []byte("disable_api_key_auth = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := grok().Discover("")
	if err != nil {
		t.Fatal(err)
	}
	policy := findResource(resources, core.KindPermissions, "requirements.toml")
	if policy == nil || policy.Scope != core.ScopeManaged || !policy.ReadOnly {
		t.Fatalf("Grok requirements should be managed and read-only: %#v", policy)
	}
}

func TestOpenCodeV2MCPShapeDetection(t *testing.T) {
	config := filepath.Join(t.TempDir(), "opencode.json")
	os.WriteFile(config, []byte(`{"mcp":{"servers":{"docs":{"type":"remote","url":"https://example.com"}}}}`), 0o644)
	location := openCodeMCPLocation(config, core.ScopeUser)
	if strings.Join(location.JSONPath, ".") != "mcp.servers" || location.Surface != "opencode-v2" {
		t.Fatalf("wrong v2 location: %#v", location)
	}
}

func TestDiscoverLocationGlobSkipsDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "looks.like-a-file"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "actual.go")
	if err := os.WriteFile(file, []byte("package actual"), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := discoverLocation(core.OpenCode, "", files(core.KindPlugin, core.ScopeUser, root, "*.*", "source", "opencode"))
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Path != file {
		t.Fatalf("expected only %s, got %#v", file, resources)
	}
}

func TestDiscoverMCPMissingNestedPathIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"projects": {"/project": {}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := discoverMCP(core.Claude, "/project", mcpLocation{
		Scope: core.ScopeLocal, Path: path, Format: "jsonc",
		JSONPath: []string{"projects", "/project", "mcpServers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 0 {
		t.Fatalf("expected no resources, got %#v", resources)
	}
}

func TestDirectoryResourceUsesDeclaredMainFile(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "example")
	if err := os.MkdirAll(filepath.Join(bundle, ".plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(bundle, ".plugin", "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"name":"example"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resources, err := discoverLocation(core.Claude, "", dirs(
		core.KindPlugin, core.ScopePlugin, root, ".plugin/plugin.json", "plugin-directory", "claude",
	))
	if err != nil || len(resources) != 1 {
		t.Fatalf("discover bundle: resources=%#v err=%v", resources, err)
	}
	if got := resourceMainPath(resources[0]); got != manifest {
		t.Fatalf("main path = %s, want %s", got, manifest)
	}
	driver := &genericDriver{id: core.Claude}
	if diagnostics := driver.Validate(resources[0]); len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func TestDirectoryDiscoveryIncludesSymlinkedBundles(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "linked-skill")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("---\ndescription: Linked skill.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for _, harness := range []core.Harness{core.Claude, core.Codex, core.Grok, core.Antigravity, core.OpenCode, core.Copilot} {
		resources, err := discoverLocation(harness, "", dirs(
			core.KindSkill, core.ScopeUser, root, "SKILL.md", "skill-directory", string(harness),
		))
		if err != nil {
			t.Fatalf("%s discovery failed: %v", harness, err)
		}
		if len(resources) != 1 || resources[0].Path != link {
			t.Errorf("%s did not discover symlinked skill: %#v", harness, resources)
		}
	}
}

func hasResource(resources []core.Resource, kind core.Kind, name string) bool {
	return findResource(resources, kind, name) != nil
}

func findResource(resources []core.Resource, kind core.Kind, name string) *core.Resource {
	for _, resource := range resources {
		if resource.Kind == kind && resource.Name == name {
			return &resource
		}
	}
	return nil
}
