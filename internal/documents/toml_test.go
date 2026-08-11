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
