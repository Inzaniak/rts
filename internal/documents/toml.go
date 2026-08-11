package documents

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func SetTOMLTable(raw []byte, prefix, name string, values map[string]any) ([]byte, error) {
	header := "[" + prefix + "." + quoteTOMLSegment(name) + "]"
	block := renderTOMLTable(header, values)
	start, end := findTOMLTable(raw, header)
	if start >= 0 {
		return splice(raw, start, end, block), nil
	}
	out := append([]byte(nil), raw...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if len(bytes.TrimSpace(out)) > 0 {
		out = append(out, '\n')
	}
	out = append(out, block...)
	return out, nil
}

func DeleteTOMLTable(raw []byte, prefix, name string) ([]byte, error) {
	header := "[" + prefix + "." + quoteTOMLSegment(name) + "]"
	start, end := findTOMLTable(raw, header)
	if start < 0 {
		return nil, fmt.Errorf("TOML table %s does not exist", header)
	}
	out := splice(raw, start, end, nil)
	out = bytes.TrimRight(out, "\n")
	if len(out) > 0 {
		out = append(out, '\n')
	}
	return out, nil
}

func findTOMLTable(raw []byte, header string) (int, int) {
	lines := bytes.SplitAfter(raw, []byte("\n"))
	offset := 0
	start := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if start >= 0 && strings.HasPrefix(trimmed, "[") {
			return start, offset
		}
		if trimmed == header {
			start = offset
		}
		offset += len(line)
	}
	if start >= 0 {
		return start, len(raw)
	}
	return -1, -1
}

func renderTOMLTable(header string, values map[string]any) []byte {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString(header)
	out.WriteByte('\n')
	for _, key := range keys {
		out.WriteString(key)
		out.WriteString(" = ")
		out.WriteString(renderTOMLValue(values[key]))
		out.WriteByte('\n')
	}
	return []byte(out.String())
}

func renderTOMLValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []string:
		parts := make([]string, len(typed))
		for i, item := range typed {
			parts[i] = strconv.Quote(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []any:
		parts := make([]string, len(typed))
		for i, item := range typed {
			parts[i] = renderTOMLValue(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		var keys []string
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+" = "+renderTOMLValue(typed[key]))
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return strconv.Quote(fmt.Sprint(value))
	}
}

func quoteTOMLSegment(value string) string {
	matched, _ := regexp.MatchString(`^[A-Za-z0-9_-]+$`, value)
	if matched {
		return value
	}
	return strconv.Quote(value)
}
