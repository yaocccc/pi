package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

const genericOutputRows = 8

func genericTool(name string, tools ...*ToolViews) bool {
	switch visualName(name) {
	case "read", "bash", "shell", "powershell", "edit", "write", "codemode":
		return false
	default:
		return !extensionPolicy(name, firstTools(tools)).Specialized
	}
}

func genericToolRows(item session.Item, theme config.Theme, width int) []string {
	var rows []string
	if args, valid := visualJSON(item.Arguments); !valid {
		rows = append(rows, fit(paint("Parameters unavailable", theme.Warning), width))
	} else if args != nil {
		// Reuse the safe field renderer, then collapse parameters to one dim line.
		// It omits binary image payloads and does not dump raw protocol JSON.
		params := toolVisual{theme: config.Theme{}, width: max(80, width)}
		params.structured(args, 0, "")
		var fields []string
		for _, row := range params.rows {
			if text := strings.TrimSpace(ansi.Strip(row)); text != "" {
				fields = append(fields, text)
			}
		}
		if len(fields) > 0 {
			rows = append(rows, fit(paint("("+strings.Join(fields, " · ")+")", visualDim(theme)), width))
		}
	}
	output := toolVisual{theme: theme, width: width}
	if item.Text != "" {
		if value, valid := visualJSON([]byte(item.Text)); valid {
			output.structured(value, 0, "")
		} else {
			output.literal(item.Text)
		}
	} else if item.Running {
		output.line("Waiting for output…", theme.Warning)
	} else if item.HasResult {
		output.line("(empty result)", visualDim(theme))
	} else {
		output.line("No result recorded", visualDim(theme))
	}
	if item.Error {
		rows = append(rows, fit(paint("Failed", theme.Error), width))
	}
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	rows = append(rows, output.rows[:min(len(output.rows), genericOutputRows)]...)
	if len(output.rows) > genericOutputRows {
		rows = append(rows, fit(paint("… (truncated)", theme.Muted), width))
	}
	return rows
}
