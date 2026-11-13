package tooldetail

import (
	"encoding/json"
	"fmt"
	"pi-goui/internal/render/tooldata"
	"sort"
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

var copyMap = tooldata.CopyMap
var decodeJSON = tooldata.DecodeJSON
var decodeMap = tooldata.DecodeMap
var stringField = tooldata.StringField
var intField = tooldata.IntField
var toolName = tooldata.ToolName

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
