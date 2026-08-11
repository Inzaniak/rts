package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/documents"
)

const redacted = "<redacted>"

var assignmentPattern = regexp.MustCompile(`(?im)^(\s*["']?[^="'\r\n]+["']?\s*[:=]\s*)(["']?)([^"'\r\n]*)(["']?)\s*(,?)$`)

// RedactResource returns a copy safe for terminal and JSON output.
func RedactResource(resource core.Resource) core.Resource {
	if resource.Metadata == nil {
		return resource
	}
	raw, err := json.Marshal(resource.Metadata)
	if err != nil {
		resource.Metadata = map[string]any{"redactionError": "metadata could not be rendered safely"}
		return resource
	}
	var metadata map[string]any
	if json.Unmarshal(raw, &metadata) == nil {
		resource.Metadata = redactMap(metadata, false)
	}
	return resource
}

// RedactResources returns output-safe copies without mutating the inventory.
func RedactResources(resources []core.Resource) []core.Resource {
	result := make([]core.Resource, len(resources))
	for index, resource := range resources {
		result[index] = RedactResource(resource)
	}
	return result
}

// ReadRedacted reads a resource and masks likely credential values.
func (s *Service) ReadRedacted(resource core.Resource) ([]byte, error) {
	content, err := s.Read(resource)
	if err != nil {
		return nil, err
	}
	return RedactContent(content), nil
}

func RedactContent(content []byte) []byte {
	var value any
	if documents.DecodeJSONC(content, &value) == nil {
		return core.PrettyJSON(redactValue(value, false))
	}
	return assignmentPattern.ReplaceAllFunc(content, func(line []byte) []byte {
		match := assignmentPattern.FindSubmatch(line)
		if len(match) != 6 {
			return line
		}
		key := strings.TrimSpace(strings.Trim(string(match[1]), `"'`+" \t:="))
		value := string(match[3])
		if !sensitiveKey(key) || isReference(value) {
			return line
		}
		return append(append(append(append(append([]byte{}, match[1]...), match[2]...), []byte(redacted)...), match[4]...), match[5]...)
	})
}

func redactValue(value any, sensitiveParent bool) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactMap(typed, sensitiveParent)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactValue(item, sensitiveParent)
		}
		return result
	case string:
		if sensitiveParent && !isReference(typed) {
			return redacted
		}
	}
	return value
}

func redactMap(value map[string]any, sensitiveParent bool) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		sensitive := sensitiveParent || sensitiveKey(key) || strings.EqualFold(key, "headers") || strings.EqualFold(key, "env")
		result[key] = redactValue(item, sensitive)
	}
	return result
}

func sensitiveKey(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(key))
	for _, marker := range []string{"token", "secret", "password", "passwd", "authorization", "apikey", "privatekey", "credential", "cookie"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func isReference(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") ||
		(strings.HasPrefix(value, "$") && !strings.ContainsAny(value, " \t")) ||
		strings.HasPrefix(value, "{env:")
}
