package service

import (
	"strings"
	"testing"

	"github.com/Inzaniak/rts/internal/core"
)

func TestRedactContentMasksLiteralSecretsAndPreservesReferences(t *testing.T) {
	input := []byte(`{
  "headers": {"Authorization": "Bearer literal"},
  "env": {"API_TOKEN": "literal", "SAFE_REF": "${SAFE_REF}"},
  "command": "server"
}`)
	output := string(RedactContent(input))
	if strings.Contains(output, "Bearer literal") || strings.Contains(output, `"literal"`) {
		t.Fatalf("literal secret leaked:\n%s", output)
	}
	if !strings.Contains(output, "${SAFE_REF}") || !strings.Contains(output, "server") {
		t.Fatalf("reference or non-secret value was changed:\n%s", output)
	}
}

func TestRedactResourceDoesNotMutateOriginal(t *testing.T) {
	resource := core.Resource{Metadata: map[string]any{
		"config": map[string]any{"apiKey": "top-secret"},
	}}
	safe := RedactResource(resource)
	if safe.Metadata["config"].(map[string]any)["apiKey"] != redacted {
		t.Fatal("safe copy was not redacted")
	}
	if resource.Metadata["config"].(map[string]any)["apiKey"] != "top-secret" {
		t.Fatal("original resource was mutated")
	}
}
