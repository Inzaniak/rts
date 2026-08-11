package documents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type member struct {
	key                  string
	start, end           int
	valueStart, valueEnd int
}

type scanner struct {
	data []byte
}

func SetJSONEntry(raw []byte, path []string, name string, value any) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		root := map[string]any{}
		cursor := root
		for _, segment := range path {
			next := map[string]any{}
			cursor[segment] = next
			cursor = next
		}
		cursor[name] = value
		return marshalJSON(root)
	}
	s := scanner{data: raw}
	start := s.skip(0)
	if start >= len(raw) || raw[start] != '{' {
		return nil, fmt.Errorf("JSON root must be an object")
	}
	objectStart := start
	for index, segment := range path {
		members, end, err := s.object(objectStart)
		if err != nil {
			return nil, err
		}
		found := findMember(members, segment)
		if found == nil {
			nested := any(map[string]any{name: value})
			for i := len(path) - 1; i > index; i-- {
				nested = map[string]any{path[i]: nested}
			}
			return insertObjectMember(raw, objectStart, end, members, segment, nested)
		}
		valueAt := s.skip(found.valueStart)
		if valueAt >= len(raw) || raw[valueAt] != '{' {
			return nil, fmt.Errorf("JSON path %s is not an object", strings.Join(path[:index+1], "."))
		}
		objectStart = valueAt
	}
	members, end, err := s.object(objectStart)
	if err != nil {
		return nil, err
	}
	if existing := findMember(members, name); existing != nil {
		encoded, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		return splice(raw, existing.valueStart, existing.valueEnd, encoded), nil
	}
	return insertObjectMember(raw, objectStart, end, members, name, value)
}

func DeleteJSONEntry(raw []byte, path []string, name string) ([]byte, error) {
	s := scanner{data: raw}
	objectStart := s.skip(0)
	for index, segment := range path {
		members, _, err := s.object(objectStart)
		if err != nil {
			return nil, err
		}
		found := findMember(members, segment)
		if found == nil {
			return nil, fmt.Errorf("JSON path %s does not exist", strings.Join(path[:index+1], "."))
		}
		objectStart = s.skip(found.valueStart)
	}
	members, _, err := s.object(objectStart)
	if err != nil {
		return nil, err
	}
	for index := range members {
		if members[index].key != name {
			continue
		}
		start, end := members[index].start, members[index].end
		if index+1 < len(members) {
			end = members[index+1].start
		} else if index > 0 {
			start = members[index-1].valueEnd
		}
		out := splice(raw, start, end, nil)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		return out, nil
	}
	return nil, fmt.Errorf("entry %q does not exist", name)
}

func DecodeJSONC(raw []byte, target any) error {
	clean := stripComments(raw)
	clean = stripTrailingCommas(clean)
	if err := json.Unmarshal(clean, target); err != nil {
		return fmt.Errorf("parse JSON/JSONC: %w", err)
	}
	return nil
}

func insertObjectMember(raw []byte, objectStart, objectEnd int, members []member, key string, value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	lineStart := bytes.LastIndex(raw[:objectEnd], []byte("\n")) + 1
	closeIndent := leadingWhitespace(string(raw[lineStart:objectEnd]))
	childIndent := closeIndent + "  "
	encoded = indentLines(encoded, childIndent)
	var insertion strings.Builder
	if len(members) > 0 {
		insertion.WriteByte(',')
	}
	insertion.WriteByte('\n')
	insertion.WriteString(childIndent)
	insertion.WriteString(strconv.Quote(key))
	insertion.WriteString(": ")
	insertion.Write(encoded)
	insertion.WriteByte('\n')
	insertion.WriteString(closeIndent)
	return splice(raw, objectEnd, objectEnd, []byte(insertion.String())), nil
}

func leadingWhitespace(value string) string {
	for index, r := range value {
		if r != ' ' && r != '\t' {
			return value[:index]
		}
	}
	return value
}

func indentLines(value []byte, indent string) []byte {
	lines := bytes.Split(value, []byte("\n"))
	if len(lines) == 1 {
		return value
	}
	var out bytes.Buffer
	for index, line := range lines {
		if index > 0 {
			out.WriteByte('\n')
			out.WriteString(indent)
		}
		out.Write(line)
	}
	return out.Bytes()
}

func marshalJSON(value any) ([]byte, error) {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func findMember(members []member, key string) *member {
	for index := range members {
		if members[index].key == key {
			return &members[index]
		}
	}
	return nil
}

func (s scanner) object(start int) ([]member, int, error) {
	start = s.skip(start)
	if start >= len(s.data) || s.data[start] != '{' {
		return nil, 0, fmt.Errorf("expected object at byte %d", start)
	}
	pos := s.skip(start + 1)
	var members []member
	for pos < len(s.data) && s.data[pos] != '}' {
		entry := member{start: pos}
		keyEnd, key, err := s.string(pos)
		if err != nil {
			return nil, 0, err
		}
		entry.key = key
		pos = s.skip(keyEnd)
		if pos >= len(s.data) || s.data[pos] != ':' {
			return nil, 0, fmt.Errorf("expected colon after %q", key)
		}
		entry.valueStart = s.skip(pos + 1)
		entry.valueEnd, err = s.valueEnd(entry.valueStart)
		if err != nil {
			return nil, 0, err
		}
		pos = s.skip(entry.valueEnd)
		entry.end = pos
		if pos < len(s.data) && s.data[pos] == ',' {
			pos = s.skip(pos + 1)
			entry.end = pos
		}
		members = append(members, entry)
	}
	if pos >= len(s.data) || s.data[pos] != '}' {
		return nil, 0, fmt.Errorf("unterminated JSON object")
	}
	return members, pos, nil
}

func (s scanner) valueEnd(start int) (int, error) {
	if start >= len(s.data) {
		return 0, fmt.Errorf("missing JSON value")
	}
	if s.data[start] == '"' {
		end, _, err := s.string(start)
		return end, err
	}
	if s.data[start] == '{' || s.data[start] == '[' {
		open := s.data[start]
		close := byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		for pos := start; pos < len(s.data); pos++ {
			switch s.data[pos] {
			case '"':
				end, _, err := s.string(pos)
				if err != nil {
					return 0, err
				}
				pos = end - 1
			case '/':
				next := s.skipComment(pos)
				if next > pos {
					pos = next - 1
				}
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return pos + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("unterminated JSON value")
	}
	pos := start
	for pos < len(s.data) && s.data[pos] != ',' && s.data[pos] != '}' && s.data[pos] != ']' {
		pos++
	}
	return trimRightSpace(s.data, start, pos), nil
}

func (s scanner) string(start int) (int, string, error) {
	if start >= len(s.data) || s.data[start] != '"' {
		return 0, "", fmt.Errorf("expected JSON string at byte %d", start)
	}
	for pos := start + 1; pos < len(s.data); pos++ {
		if s.data[pos] == '\\' {
			pos++
			continue
		}
		if s.data[pos] == '"' {
			var value string
			if err := json.Unmarshal(s.data[start:pos+1], &value); err != nil {
				return 0, "", err
			}
			return pos + 1, value, nil
		}
	}
	return 0, "", fmt.Errorf("unterminated JSON string")
}

func (s scanner) skip(pos int) int {
	for pos < len(s.data) {
		switch s.data[pos] {
		case ' ', '\t', '\r', '\n':
			pos++
		case '/':
			next := s.skipComment(pos)
			if next == pos {
				return pos
			}
			pos = next
		default:
			return pos
		}
	}
	return pos
}

func (s scanner) skipComment(pos int) int {
	if pos+1 >= len(s.data) || s.data[pos] != '/' {
		return pos
	}
	switch s.data[pos+1] {
	case '/':
		pos += 2
		for pos < len(s.data) && s.data[pos] != '\n' {
			pos++
		}
		return pos
	case '*':
		pos += 2
		for pos+1 < len(s.data) {
			if s.data[pos] == '*' && s.data[pos+1] == '/' {
				return pos + 2
			}
			pos++
		}
		return len(s.data)
	default:
		return pos
	}
}

func splice(raw []byte, start, end int, replacement []byte) []byte {
	out := make([]byte, 0, len(raw)-(end-start)+len(replacement))
	out = append(out, raw[:start]...)
	out = append(out, replacement...)
	out = append(out, raw[end:]...)
	return out
}

func trimRightSpace(raw []byte, start, end int) int {
	for end > start {
		switch raw[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return end
		}
	}
	return end
}

func stripComments(raw []byte) []byte {
	out := append([]byte(nil), raw...)
	inString := false
	for i := 0; i < len(out); i++ {
		if out[i] == '"' {
			escaped := false
			for j := i - 1; j >= 0 && out[j] == '\\'; j-- {
				escaped = !escaped
			}
			if !escaped {
				inString = !inString
			}
			continue
		}
		if inString || out[i] != '/' || i+1 >= len(out) {
			continue
		}
		if out[i+1] == '/' {
			out[i], out[i+1] = ' ', ' '
			for i+2 < len(out) && out[i+2] != '\n' {
				i++
				out[i+1] = ' '
			}
		} else if out[i+1] == '*' {
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/') {
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
			}
		}
	}
	return out
}

func stripTrailingCommas(raw []byte) []byte {
	out := append([]byte(nil), raw...)
	inString := false
	for i := 0; i < len(out); i++ {
		if out[i] == '"' {
			escaped := false
			for j := i - 1; j >= 0 && out[j] == '\\'; j-- {
				escaped = !escaped
			}
			if !escaped {
				inString = !inString
			}
			continue
		}
		if inString || out[i] != ',' {
			continue
		}
		j := i + 1
		for j < len(out) && bytes.ContainsRune([]byte(" \t\r\n"), rune(out[j])) {
			j++
		}
		if j < len(out) && (out[j] == '}' || out[j] == ']') {
			out[i] = ' '
		}
	}
	return out
}
