package documents

import (
	"strings"
	"testing"
)

func TestSetJSONEntryPreservesCommentsAndUnknownFields(t *testing.T) {
	raw := []byte(`{
  // keep this explanation
  "unknownFutureSetting": true,
  "mcpServers": {
    "existing": {
      "command": "old"
    }
  }
}
`)
	updated, err := SetJSONEntry(raw, []string{"mcpServers"}, "docs", map[string]any{
		"url": "https://example.com/mcp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "// keep this explanation") {
		t.Fatalf("comment was lost:\n%s", updated)
	}
	var decoded map[string]any
	if err := DecodeJSONC(updated, &decoded); err != nil {
		t.Fatalf("updated JSONC is invalid: %v\n%s", err, updated)
	}
	if decoded["unknownFutureSetting"] != true {
		t.Fatal("unknown field was changed")
	}
	servers := decoded["mcpServers"].(map[string]any)
	if _, ok := servers["docs"]; !ok {
		t.Fatal("new MCP entry was not added")
	}
}

func TestSetJSONEntryCreatesNestedObjectsInCompactDocument(t *testing.T) {
	updated, err := SetJSONEntry([]byte("{}\n"), []string{"mcp", "servers"}, "docs", map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := DecodeJSONC(updated, &decoded); err != nil {
		t.Fatalf("invalid output: %v\n%s", err, updated)
	}
	mcp := decoded["mcp"].(map[string]any)
	servers := mcp["servers"].(map[string]any)
	if _, ok := servers["docs"]; !ok {
		t.Fatal("nested entry missing")
	}
}

func TestDeleteJSONEntryFirstMiddleAndLast(t *testing.T) {
	for _, name := range []string{"first", "middle", "last"} {
		t.Run(name, func(t *testing.T) {
			raw := []byte(`{
  "mcpServers": {
    "first": {"command": "a"},
    "middle": {"command": "b"},
    "last": {"command": "c"}
  },
  "keep": 42
}
`)
			updated, err := DeleteJSONEntry(raw, []string{"mcpServers"}, name)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := DecodeJSONC(updated, &decoded); err != nil {
				t.Fatalf("invalid output: %v\n%s", err, updated)
			}
			servers := decoded["mcpServers"].(map[string]any)
			if _, ok := servers[name]; ok {
				t.Fatalf("%s was not deleted", name)
			}
			if decoded["keep"].(float64) != 42 {
				t.Fatal("unrelated field changed")
			}
		})
	}
}
