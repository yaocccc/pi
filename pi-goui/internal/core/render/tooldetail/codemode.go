package tooldetail

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"pi-goui/internal/core/session"
)

// Match only the executor's generated envelope, as the transcript renderer
// does. Do not trim the remaining output: leading and trailing blank lines are
// part of the result, not layout whitespace.
var scriptEnvelope = regexp.MustCompile(`^Script (completed|failed)\nWall time ([0-9]+(?:\.[0-9]+)?) seconds\nOutput:\n`)

func (v *renderer) codemode(item session.Item, args, details map[string]any) {
	code := stringField(args, "code", "script")
	if code != "" {
		v.heading("› Script")
		v.code(code, "", "javascript", 1)
	}
	parameters := copyMap(args)
	for _, key := range []string{"code", "script"} {
		if value, ok := parameters[key].(string); ok && value == code {
			delete(parameters, key)
			break
		}
	}
	if len(parameters) > 0 {
		v.line("Parameters", v.theme.Muted)
		v.value(parameters, "")
	}
	output := strings.ReplaceAll(item.Text, "\r\n", "\n")
	match := scriptEnvelope.FindStringSubmatch(output)
	switch {
	case item.Running:
		v.line("⋯ Running script…", v.theme.Warning)
	case match != nil && match[1] == "failed":
		v.line("✗ Script failed", v.theme.Error)
	case match != nil && match[1] == "completed":
		v.line("✓ Script completed", v.theme.Success)
	case item.HasResult:
		v.line("■ Script finished", v.theme.Muted)
	default:
		v.line("○ Preparing script", v.theme.Muted)
	}
	if match != nil {
		v.line("Wall time · "+match[2]+" seconds", v.theme.Muted)
		output = output[len(match[0]):]
	}
	v.calls(details)
	v.output(output)
	if output == "" {
		if item.Running || !item.HasResult {
			v.line("(waiting for output)", v.theme.Muted)
		} else {
			v.line("(no output)", v.theme.Muted)
		}
	}
}

func (v *renderer) calls(details map[string]any) {
	calls, ok := details["calls"].([]any)
	if !ok {
		return
	}
	v.heading(fmt.Sprintf("Tool calls · %d", len(calls)))
	for i, value := range calls {
		call, ok := value.(map[string]any)
		if !ok {
			v.value(value, "")
			continue
		}
		call = copyMap(call)
		title := fmt.Sprintf("#%d · %s", i+1, stringField(call, "name"))
		if _, ok := call["name"].(string); ok {
			delete(call, "name")
		}
		if status, ok := call["status"].(string); ok {
			icon, color := "○", v.theme.Muted
			switch status {
			case "ok", "completed":
				icon, color = "✓", v.theme.Success
			case "running":
				icon, color = "⋯", v.theme.Warning
			case "error", "failed":
				icon, color = "✗", v.theme.Error
			case "cancelled":
				icon = "⊘"
			}
			// The transcript's trace puts status and tool identity on one
			// compact line; keep the exact status instead of inferring it.
			v.line(title+" · "+icon+" "+status, color)
			delete(call, "status")
		} else {
			v.heading(title)
			v.field(call, "status", "Status")
		}
		if duration, ok := call["durationMs"].(json.Number); ok {
			v.line("durationMs: "+duration.String()+" ms", v.theme.Muted)
			delete(call, "durationMs")
		}
		if cost, ok := call["cost"].(json.Number); ok {
			// Exact decimal cost, not a rounded transcript badge.
			v.line("cost: $"+cost.String(), v.theme.Muted)
			delete(call, "cost")
		}
		v.callValue(call, "args", "Arguments")
		if failure, ok := call["error"].(string); ok {
			v.line("Error", v.theme.Error)
			v.line(failure, v.theme.Error)
			delete(call, "error")
		} else {
			v.field(call, "error", "Error")
		}
		v.callValue(call, "result", "Result")
		if len(call) > 0 {
			v.value(call, "")
		}
	}
	delete(details, "calls")
}

// Calls commonly encode their arguments as JSON strings. Show their structure
// without coercing ordinary text or scalar strings into different values.
func (v *renderer) callValue(call map[string]any, key, label string) {
	value, ok := call[key]
	if !ok {
		return
	}
	v.heading(label)
	if text, ok := value.(string); ok {
		if decoded, valid := decodeJSON([]byte(text)); valid {
			switch decoded.(type) {
			case map[string]any, []any:
				v.value(decoded, "")
				delete(call, key)
				return
			}
		}
	}
	v.value(value, "")
	delete(call, key)
}
