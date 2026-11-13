package detail

import (
	"strings"

	"pi-goui/internal/core/session"
)

// webEnable reflects activation results; availability is not inferred from the
// requested tools, and activation does not imply a search or fetch occurred.
func (v *Renderer) WebEnable(item session.Item, args, details map[string]any) {
	v.Heading("Web access")
	if item.Running {
		v.Line("● Enabling configured tools", v.Theme.Warning)
	}
	if len(args) > 0 {
		v.Heading("Parameters")
		v.Value(args, "")
	}
	enabledNames := []string{}
	exactEnabled := false
	for _, field := range []struct{ key, label, marker, color string }{
		{"enabled", "Enabled tools", "✓ ", v.Theme.Success},
		{"unavailable", "Unavailable tools", "× ", v.Theme.Error},
		{"missing", "Still inactive", "○ ", v.Theme.Warning},
	} {
		data, exists := details[field.key]
		if !exists {
			continue
		}
		v.Heading(field.label)
		if list, ok := data.([]any); ok {
			valid := true
			for _, value := range list {
				if name, ok := value.(string); ok {
					v.Line(field.marker+name, field.color)
					if field.key == "enabled" {
						enabledNames = append(enabledNames, name)
					}
				} else {
					valid = false
					v.Value(value, "")
				}
			}
			if len(list) == 0 {
				v.Line("(none)", v.Theme.Muted)
			}
			if field.key == "enabled" {
				exactEnabled = valid
			}
		} else {
			v.Value(data, "")
		}
		delete(details, field.key)
	}
	v.Field(details, "error", "Activation error")
	text := item.Text
	if exactEnabled && text == "Enabled: "+strings.Join(enabledNames, ", ")+"." {
		text = ""
	}
	v.Output(text)
	if item.Text == "" && !item.HasResult && !item.Running && len(enabledNames) == 0 {
		v.Line("No activation result recorded", v.Theme.Muted)
	}
}
