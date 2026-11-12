package tooldetail

import (
	"strings"

	"pi-goui/internal/session"
)

// webEnable reflects activation results; availability is not inferred from the
// requested tools, and activation does not imply a search or fetch occurred.
func (v *renderer) webEnable(item session.Item, args, details map[string]any) {
	v.heading("Web access")
	if item.Running {
		v.line("● Enabling configured tools", v.theme.Warning)
	}
	if len(args) > 0 {
		v.heading("Parameters")
		v.value(args, "")
	}
	enabledNames := []string{}
	exactEnabled := false
	for _, field := range []struct{ key, label, marker, color string }{
		{"enabled", "Enabled tools", "✓ ", v.theme.Success},
		{"unavailable", "Unavailable tools", "× ", v.theme.Error},
		{"missing", "Still inactive", "○ ", v.theme.Warning},
	} {
		data, exists := details[field.key]
		if !exists {
			continue
		}
		v.heading(field.label)
		if list, ok := data.([]any); ok {
			valid := true
			for _, value := range list {
				if name, ok := value.(string); ok {
					v.line(field.marker+name, field.color)
					if field.key == "enabled" {
						enabledNames = append(enabledNames, name)
					}
				} else {
					valid = false
					v.value(value, "")
				}
			}
			if len(list) == 0 {
				v.line("(none)", v.theme.Muted)
			}
			if field.key == "enabled" {
				exactEnabled = valid
			}
		} else {
			v.value(data, "")
		}
		delete(details, field.key)
	}
	v.field(details, "error", "Activation error")
	text := item.Text
	if exactEnabled && text == "Enabled: "+strings.Join(enabledNames, ", ")+"." {
		text = ""
	}
	v.output(text)
	if item.Text == "" && !item.HasResult && !item.Running && len(enabledNames) == 0 {
		v.line("No activation result recorded", v.theme.Muted)
	}
}
