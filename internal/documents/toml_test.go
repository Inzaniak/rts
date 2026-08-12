package documents

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

func TestTOMLTableRoundTripPreservesUnrelatedContent(t *testing.T) {
	raw := []byte(`# personal note
model = "gpt-test"

[mcp_servers.old]
command = "old"
`)
	updated, err := SetTOMLTable(raw, "mcp_servers", "docs", map[string]any{
		"url":     "https://example.com/mcp",
		"enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "# personal note") || !strings.Contains(string(updated), `model = "gpt-test"`) {
		t.Fatalf("unrelated TOML changed:\n%s", updated)
	}
	var decoded map[string]any
	if err := toml.Unmarshal(updated, &decoded); err != nil {
		t.Fatalf("invalid TOML: %v\n%s", err, updated)
	}
	servers := decoded["mcp_servers"].(map[string]any)
	if _, ok := servers["docs"]; !ok {
		t.Fatal("new table missing")
	}
	deleted, err := DeleteTOMLTable(updated, "mcp_servers", "old")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(deleted), "[mcp_servers.old]") {
		t.Fatal("old table was not deleted")
	}
	if err := toml.Unmarshal(deleted, &decoded); err != nil {
		t.Fatalf("invalid TOML after delete: %v", err)
	}
}

func TestDeleteTOMLTableRemovesNestedTables(t *testing.T) {
	raw := []byte(`model = "keep-me"

[mcp_servers.node_repl]
command = "node_repl"

[mcp_servers.node_repl.env]
NODE_PATH = "/tmp/modules"

[mcp_servers.node_repl.tools.evaluate]
approval_mode = "auto"

[mcp_servers.node_repl_extra]
command = "keep-extra"

[mcp_servers.docs]
url = "https://example.com/mcp"
`)
	deleted, err := DeleteTOMLTable(raw, "mcp_servers", "node_repl")
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{
		"[mcp_servers.node_repl]",
		"[mcp_servers.node_repl.env]",
		"[mcp_servers.node_repl.tools.evaluate]",
	} {
		if strings.Contains(string(deleted), removed) {
			t.Fatalf("nested MCP table was not deleted: %s\n%s", removed, deleted)
		}
	}
	for _, preserved := range []string{
		`model = "keep-me"`,
		"[mcp_servers.node_repl_extra]",
		"[mcp_servers.docs]",
	} {
		if !strings.Contains(string(deleted), preserved) {
			t.Fatalf("unrelated TOML was deleted: %s\n%s", preserved, deleted)
		}
	}
	var decoded map[string]any
	if err := toml.Unmarshal(deleted, &decoded); err != nil {
		t.Fatalf("invalid TOML after nested delete: %v\n%s", err, deleted)
	}
}
