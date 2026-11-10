package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// table uses a compact view only when every saved scalar fits. Nested, sparse,
// multiline or narrow results retain the lossless field-by-field presentation.
func (v *toolVisual) table(items []any, indent string) bool {
	if len(items) < 2 {
		return false
	}
	first, ok := items[0].(map[string]any)
	if !ok || len(first) < 2 || len(first) > 6 {
		return false
	}
	keys := make([]string, 0, len(first))
	for k := range first {
		// Preserve attachment redaction and source-code-specific rendering.
		if k == "data" || k == "code" || k == "command" || k == "content" {
			return false
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	widths := make([]int, len(keys))
	headers := make([]string, len(keys))
	for i, k := range keys {
		headers[i] = oneLine(visualLabel(k))
		widths[i] = ansi.StringWidth(headers[i])
	}
	rows := make([][]string, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok || len(obj) != len(keys) {
			return false
		}
		rows[i] = make([]string, len(keys))
		for j, key := range keys {
			value, exists := obj[key]
			if !exists {
				return false
			}
			switch x := value.(type) {
			case map[string]any, []any:
				return false
			case string:
				if strings.ContainsAny(x, "\n\r\t") {
					return false
				}
				if nested, valid := visualJSON([]byte(x)); valid {
					switch nested.(type) {
					case map[string]any, []any:
						return false
					}
				}
			}
			rows[i][j] = oneLine(visualScalar(value))
			widths[j] = max(widths[j], ansi.StringWidth(rows[i][j]))
		}
	}
	total := ansi.StringWidth(indent) + (len(keys)-1)*3
	for _, w := range widths {
		total += w
	}
	if total > v.width {
		return false
	}
	renderRow := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			parts[i] = cell + strings.Repeat(" ", widths[i]-ansi.StringWidth(cell))
		}
		return indent + strings.Join(parts, " │ ")
	}
	v.line(renderRow(headers), v.theme.Accent)
	rules := make([]string, len(keys))
	for i, w := range widths {
		rules[i] = strings.Repeat("─", w)
	}
	v.line(indent+strings.Join(rules, "─┼─"), v.theme.Muted)
	for _, row := range rows {
		v.line(renderRow(row), v.theme.Text)
	}
	return true
}

func resultSynopsis(value any) string {
	switch x := value.(type) {
	case map[string]any:
		if mime := visualString(x, "mimeType", "mime_type"); strings.HasPrefix(mime, "image/") {
			if _, exists := x["data"]; exists {
				return "Image attachment · " + mime
			}
		}
		if s := visualString(x, "error", "message", "summary", "status"); s != "" {
			return s
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, 3)
		for _, key := range keys {
			var s string
			switch child := x[key].(type) {
			case []any:
				s = fmt.Sprintf("%d items", len(child))
			case map[string]any:
				s = fmt.Sprintf("%d fields", len(child))
			default:
				s = visualScalar(child)
			}
			parts = append(parts, visualLabel(key)+": "+s)
			if len(parts) == 3 {
				break
			}
		}
		if len(parts) == 0 {
			return "empty object"
		}
		if len(keys) > len(parts) {
			parts = append(parts, fmt.Sprintf("+%d fields", len(keys)-len(parts)))
		}
		return strings.Join(parts, " · ")
	case []any:
		return fmt.Sprintf("%d result item(s)", len(x))
	default:
		return visualScalar(value)
	}
}
