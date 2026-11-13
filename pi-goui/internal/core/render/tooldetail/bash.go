package tooldetail

import (
	"encoding/json"
	"strings"

	"pi-goui/internal/core/session"
)

// Keep the command first, like the transcript's terminal card. The inspector
// adds a compact execution footer, but never applies the card's output limit.
func (v *renderer) bash(item session.Item, args, details map[string]any) {
	command := v.commandInvocation(item, args)
	if command {
		switch {
		case item.Running:
			v.line("⋯ Running…", v.theme.Warning)
		case item.HasResult:
			// A completed tool call does not by itself prove a zero exit code.
			v.line("■ Finished", v.theme.Muted)
		default:
			v.line("○ Preparing command", v.theme.Muted)
		}
	}
	v.commandExit(details)
	v.output(item.Text)
	if command && item.Text == "" {
		if item.Running || !item.HasResult {
			v.line("(waiting for output)", v.theme.Muted)
		} else {
			v.line("(no output)", v.theme.Muted)
		}
	}
}

// The shared dispatcher prints failure text first; retain the invocation and
// metadata without introducing a second, apparently successful result section.
func (v *renderer) failedCommand(item session.Item, args, details map[string]any) {
	v.commandInvocation(item, args)
	v.commandExit(details)
}

func (v *renderer) commandInvocation(item session.Item, args map[string]any) bool {
	command := stringField(args, "command", "cmd", "script")
	language, prompt := "bash", "$"
	if toolName(item.ToolName) == "powershell" {
		language, prompt = "powershell", "PS>"
	}
	if command != "" {
		v.line(prompt+" Command", v.theme.Accent)
		v.code(command, "", language, 1)
	}
	// Transcript cards include timeout/cwd beside the command. Put them below
	// it here so arbitrarily long or nested parameters remain fully readable.
	parameters := copyMap(args)
	for _, key := range []string{"command", "cmd", "script"} {
		if value, ok := parameters[key].(string); ok && value == command {
			delete(parameters, key)
			break
		}
	}
	if len(parameters) > 0 {
		v.line("Parameters", v.theme.Muted)
		v.value(parameters, "")
	}
	return command != ""
}

func (v *renderer) commandExit(details map[string]any) {
	value, ok := details["exitCode"]
	if !ok {
		return
	}
	color, mark := v.theme.Muted, "○"
	if number, ok := value.(json.Number); ok {
		if n, err := number.Int64(); err == nil {
			color, mark = v.theme.Success, "✓"
			if n != 0 {
				color, mark = v.theme.Error, "✗"
			}
		}
	}
	switch value.(type) {
	case map[string]any, []any:
		v.heading("Exit code")
		v.value(value, "")
	default:
		// Exact values, including nonstandard values, remain available; no
		// floating-point conversion or guessing success from a string value.
		v.line(strings.Join([]string{mark, "Exit code ·", v.scalar(value)}, " "), color)
	}
	delete(details, "exitCode")
}
