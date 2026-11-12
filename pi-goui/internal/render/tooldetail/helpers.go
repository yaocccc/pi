package tooldetail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

func (v *renderer) line(text, color string) {
	v.rows = append(v.rows, v.hooks.Line(text, color, v.width)...)
}

func (v *renderer) heading(text string) { v.line(text, v.theme.Accent) }
func (v *renderer) code(source, path, language string, start int) {
	v.rows = append(v.rows, v.hooks.CodeRows(source, path, language, start, v.width, v.theme)...)
}
func (v *renderer) output(text string) {
	if text != "" {
		v.heading("Output")
		v.line(text, v.theme.Text)
	}
}

// Unknown fields stay visible, recursively, with no row/depth/array caps.
// Keys keep their source spelling so plugin-specific metadata remains identifiable.
func (v *renderer) value(value any, indent string) {
	// Synthetic indentation must not fill a narrow row by itself.
	indent = indent[:min(len(indent), max(0, v.width-8))]
	switch x := value.(type) {
	case map[string]any:
		if len(x) == 0 {
			v.line(indent+"{}", v.theme.Muted)
			return
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			switch child := x[key].(type) {
			case map[string]any, []any:
				v.line(indent+key, v.theme.Accent)
				v.value(child, indent+"  ")
			default:
				v.line(indent+key+": "+v.scalar(child), v.theme.Text)
			}
		}
	case []any:
		if len(x) == 0 {
			v.line(indent+"[]", v.theme.Muted)
			return
		}
		for i, child := range x {
			switch child.(type) {
			case map[string]any, []any:
				v.line(fmt.Sprintf("%s#%d", indent, i+1), v.theme.Muted)
				v.value(child, indent+"  ")
			default:
				v.line(indent+"• "+v.scalar(child), v.theme.Text)
			}
		}
	default:
		v.line(indent+v.scalar(x), v.theme.Text)
	}
}
func (v *renderer) field(m map[string]any, key, label string) {
	if value, ok := m[key]; ok {
		v.heading(label)
		v.value(value, "")
		delete(m, key)
	}
}
func copyMap(m map[string]any) map[string]any {
	copy := make(map[string]any, len(m))
	for k, value := range m {
		copy[k] = value
	}
	return copy
}

func decodeJSON(data []byte) (any, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, true
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return v, true
}
func decodeMap(raw json.RawMessage) map[string]any {
	v, ok := decodeJSON(raw)
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}
func stringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func intField(m map[string]any, key string, fallback int) int {
	v := m[key]
	switch n := v.(type) {
	case json.Number:
		i, e := strconv.Atoi(string(n))
		if e == nil && i > 0 && i < 1<<30 {
			return i
		}
	case float64:
		if n > 0 && n < 1<<30 {
			return int(n)
		}
	}
	return fallback
}
func toolName(name string) string {
	name = strings.ToLower(name)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
func (v *renderer) scalar(value any) string {
	switch x := value.(type) {
	case nil:
		return "—"
	case string:
		return v.hooks.Clean(x)
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case json.Number:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
