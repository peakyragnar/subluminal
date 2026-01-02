package policy

import (
	"fmt"
	"strconv"
	"strings"
)

type yamlFrame struct {
	indent     int
	container  any
	pendingKey string
	parentMap  map[string]any
	parentKey  string
}

func parseYAMLBundle(input string) (any, error) {
	lines := strings.Split(input, "\n")
	root := map[string]any{}
	stack := []yamlFrame{{indent: -1, container: root}}

	for idx, raw := range lines {
		lineNo := idx + 1
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent, err := countIndent(raw)
		if err != nil {
			return nil, fmt.Errorf("yaml line %d: %v", lineNo, err)
		}

		stripped := stripComments(raw)
		if strings.TrimSpace(stripped) == "" {
			continue
		}

		trimmed := strings.TrimSpace(stripped)

		for len(stack) > 1 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}

		frame := &stack[len(stack)-1]
		if indent > frame.indent {
			if frame.pendingKey != "" {
				container := newContainerForLine(trimmed)
				parent, ok := frame.container.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("yaml line %d: expected map for nested key", lineNo)
				}
				key := frame.pendingKey
				parent[key] = container
				frame.pendingKey = ""
				stack = append(stack, yamlFrame{
					indent:    indent,
					container: container,
					parentMap: parent,
					parentKey: key,
				})
				frame = &stack[len(stack)-1]
			} else if frame.indent >= 0 {
				return nil, fmt.Errorf("yaml line %d: unexpected indentation", lineNo)
			}
		} else {
			frame.pendingKey = ""
		}

		if strings.HasPrefix(trimmed, "-") {
			list, ok := frame.container.([]any)
			if !ok {
				return nil, fmt.Errorf("yaml line %d: list item without list context", lineNo)
			}

			itemText := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
			if itemText == "" {
				list = append(list, nil)
				updateListFrame(frame, list)
				continue
			}

			if key, val, ok := splitKeyValue(itemText); ok {
				itemMap := map[string]any{}
				if val == "" {
					itemMap[key] = nil
				} else {
					parsed, err := parseValue(val)
					if err != nil {
						return nil, fmt.Errorf("yaml line %d: %v", lineNo, err)
					}
					itemMap[key] = parsed
				}
				list = append(list, itemMap)
				updateListFrame(frame, list)

				newFrame := yamlFrame{indent: indent, container: itemMap}
				if val == "" {
					newFrame.pendingKey = key
				}
				stack = append(stack, newFrame)
				continue
			}

			parsed, err := parseValue(itemText)
			if err != nil {
				return nil, fmt.Errorf("yaml line %d: %v", lineNo, err)
			}
			list = append(list, parsed)
			updateListFrame(frame, list)
			continue
		}

		// Map entry
		m, ok := frame.container.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("yaml line %d: expected map context", lineNo)
		}
		key, val, ok := splitKeyValue(trimmed)
		if !ok {
			return nil, fmt.Errorf("yaml line %d: invalid key/value", lineNo)
		}
		if val == "" {
			m[key] = nil
			frame.pendingKey = key
			continue
		}
		parsed, err := parseValue(val)
		if err != nil {
			return nil, fmt.Errorf("yaml line %d: %v", lineNo, err)
		}
		m[key] = parsed
	}

	return root, nil
}

func updateListFrame(frame *yamlFrame, list []any) {
	frame.container = list
	if frame.parentMap != nil {
		frame.parentMap[frame.parentKey] = list
	}
}

func newContainerForLine(line string) any {
	if strings.HasPrefix(strings.TrimSpace(line), "-") {
		return []any{}
	}
	return map[string]any{}
}

func countIndent(line string) (int, error) {
	count := 0
	for _, r := range line {
		switch r {
		case ' ':
			count++
		case '\t':
			return 0, fmt.Errorf("tabs not supported")
		default:
			return count, nil
		}
	}
	return count, nil
}

func stripComments(line string) string {
	var (
		inSingle bool
		inDouble bool
	)
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func splitKeyValue(line string) (string, string, bool) {
	inSingle := false
	inDouble := false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if !inSingle && !inDouble {
				key := strings.TrimSpace(line[:i])
				val := strings.TrimSpace(line[i+1:])
				return key, val, true
			}
		}
	}
	return "", "", false
}

func parseValue(raw string) (any, error) {
	val := strings.TrimSpace(raw)
	if val == "" {
		return "", nil
	}

	if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
		return parseInlineList(val[1 : len(val)-1])
	}
	if strings.HasPrefix(val, "{") && strings.HasSuffix(val, "}") {
		return parseInlineMap(val[1 : len(val)-1])
	}

	if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
		return strconv.Unquote(val)
	}
	if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") {
		inner := strings.TrimSuffix(strings.TrimPrefix(val, "'"), "'")
		return strings.ReplaceAll(inner, "''", "'"), nil
	}

	switch strings.ToLower(val) {
	case "null", "~":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}

	if i, err := strconv.ParseInt(val, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f, nil
	}

	return val, nil
}

func parseInlineList(raw string) ([]any, error) {
	parts := splitInline(raw)
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		val, err := parseValue(part)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
	return out, nil
}

func parseInlineMap(raw string) (map[string]any, error) {
	out := map[string]any{}
	for _, part := range splitInline(raw) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		key, val, ok := splitKeyValue(part)
		if !ok {
			return nil, fmt.Errorf("invalid inline map entry: %q", part)
		}
		parsed, err := parseValue(val)
		if err != nil {
			return nil, err
		}
		out[key] = parsed
	}
	return out, nil
}

func splitInline(raw string) []string {
	var (
		parts    []string
		builder  strings.Builder
		depth    int
		inSingle bool
		inDouble bool
	)

	for _, r := range raw {
		switch r {
		case '[':
			if !inSingle && !inDouble {
				depth++
			}
		case ']':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		case '{':
			if !inSingle && !inDouble {
				depth++
			}
		case '}':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ',':
			if depth == 0 && !inSingle && !inDouble {
				parts = append(parts, builder.String())
				builder.Reset()
				continue
			}
		}
		builder.WriteRune(r)
	}

	if builder.Len() > 0 {
		parts = append(parts, builder.String())
	}
	return parts
}
