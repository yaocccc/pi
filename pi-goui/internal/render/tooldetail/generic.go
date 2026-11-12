package tooldetail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"pi-goui/internal/session"
)

// generic follows the transcript card's parameter/result hierarchy, but never
// folds fields, clips output, or guesses a schema for an unfamiliar tool.
func (v *renderer) generic(item session.Item, args, details map[string]any) {
	v.genericParameters(item.Arguments)
	if item.Running {
		v.line("● Running", v.theme.Warning)
	}
	// Empty and whitespace-only output is text, not JSON null. Preserve its
	// original blank lines instead of manufacturing a structured value.
	if len(bytes.TrimSpace([]byte(item.Text))) != 0 {
		if value, ok := decodeJSON([]byte(item.Text)); ok {
			v.genericShape(value)
			v.heading("Result")
			v.genericValue(value, "", "")
			// Suppress only a complete object duplicate. Other details (including
			// arrays and scalar values) remain owned by the shared fallback.
			var resultJSON, detailsJSON bytes.Buffer
			if object, ok := value.(map[string]any); ok && len(object) > 0 &&
				json.Compact(&resultJSON, []byte(item.Text)) == nil && json.Compact(&detailsJSON, item.Details) == nil &&
				bytes.Equal(resultJSON.Bytes(), detailsJSON.Bytes()) {
				for key := range details {
					delete(details, key)
				}
			}
			return
		}
	}
	if item.Text != "" {
		v.output(item.Text)
	} else if item.Running {
		v.line("Waiting for output…", v.theme.Muted)
	} else if len(details) == 0 && genericEmptyDetails(item.Details) && len(item.Images) == 0 {
		if item.HasResult {
			v.line("(empty result)", v.theme.Muted)
		} else {
			v.line("No result recorded", v.theme.Muted)
		}
	}
}

func genericEmptyDetails(raw json.RawMessage) bool {
	value, valid := decodeJSON(raw)
	if !valid {
		return false
	}
	if value == nil {
		return true
	}
	object, ok := value.(map[string]any)
	return ok && len(object) == 0
}

// Parameters use the same subdued hierarchy as the compact card. Unlike its
// single-line preview, every supplied value remains available in the inspector.
func (v *renderer) genericParameters(raw json.RawMessage) {
	value, valid := decodeJSON(raw)
	if !valid {
		v.line("Parameters unavailable · incomplete or invalid payload", v.theme.Warning)
		v.line(string(raw), v.theme.Muted)
		return
	}
	if value == nil {
		return
	}
	if object, ok := value.(map[string]any); ok && len(object) == 0 {
		return
	}
	v.line("Parameters", v.theme.Muted)
	v.genericValue(value, "", v.theme.Muted)
}

func (v *renderer) genericShape(value any) {
	switch x := value.(type) {
	case map[string]any:
		v.line(fmt.Sprintf("Object · %d fields", len(x)), v.theme.Muted)
	case []any:
		v.line(fmt.Sprintf("List · %d items", len(x)), v.theme.Muted)
	}
}

// Keep plugin keys and primitive JSON spellings intact. Nested records use the
// transcript's accent labels and subdued indices instead of raw JSON braces.
// Multiline strings are rendered separately, preserving source whitespace.
func (v *renderer) genericValue(value any, indent, colorOverride string) {
	indent = indent[:min(len(indent), max(0, v.width-8))]
	line := func(text, color string) {
		if colorOverride != "" {
			color = colorOverride
		}
		v.line(text, color)
	}
	switch x := value.(type) {
	case map[string]any:
		if len(x) == 0 {
			line(indent+"{}", v.theme.Muted)
			return
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := x[key]
			switch child.(type) {
			case map[string]any, []any:
				line(indent+key, v.theme.Accent)
				v.genericValue(child, indent+"  ", colorOverride)
			default:
				text, color := v.genericScalar(child)
				if strings.Contains(text, "\n") {
					line(indent+key, v.theme.Muted)
					line(text, color)
				} else {
					line(indent+key+": "+text, color)
				}
			}
		}
	case []any:
		if len(x) == 0 {
			line(indent+"[]", v.theme.Muted)
		}
		for i, child := range x {
			switch child.(type) {
			case map[string]any, []any:
				line(fmt.Sprintf("%s#%d", indent, i+1), v.theme.Muted)
				v.genericValue(child, indent+"  ", colorOverride)
			default:
				text, color := v.genericScalar(child)
				if strings.Contains(text, "\n") {
					line(fmt.Sprintf("%s#%d", indent, i+1), v.theme.Muted)
					line(text, color)
				} else {
					line(indent+"• "+text, color)
				}
			}
		}
	default:
		text, color := v.genericScalar(x)
		line(text, color)
	}
}

func (v *renderer) genericScalar(value any) (string, string) {
	switch x := value.(type) {
	case nil:
		return "null", v.theme.Muted
	case bool:
		return fmt.Sprint(x), v.theme.Muted
	case json.Number:
		return string(x), v.theme.Accent
	default:
		return v.scalar(value), v.theme.Text
	}
}
