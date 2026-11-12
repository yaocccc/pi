// Package tooldetail renders complete tool results independently of transcript cards.
package tooldetail

import (
	"fmt"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// Hooks supplies terminal rendering primitives from the parent render package.
// All hooks are required, pure, and must preserve content at the requested width.
// Line sanitizes and styles each source line separately (no multiline padding).
type Hooks struct {
	Line       func(text, color string, width int) []string
	Clean      func(string) string
	CodeRows   func(source, path, language string, start, width int, theme config.Theme) []string
	DiffRows   func(diff string, width int, theme config.Theme) []string
	DiffCounts func(diff string) (added, removed int)
}

type renderer struct {
	theme config.Theme
	width int
	hooks Hooks
	rows  []string
}

// Render returns unabridged result rows. Each call owns its mutable display data.
func Render(item session.Item, theme config.Theme, width int, hooks Hooks) []string {
	v := renderer{theme: theme, width: max(1, width), hooks: hooks}
	v.tool(item)
	return v.rows
}

func (v *renderer) tool(item session.Item) {
	args := decodeMap(item.Arguments)
	details := copyMap(decodeMap(item.Details))
	name := toolName(item.ToolName)
	if item.Error {
		v.line("Tool failed", v.theme.Error)
		v.line(item.Text, v.theme.Error)
		if name == "bash" || name == "shell" || name == "powershell" {
			v.failedCommand(item, args, details)
		}
	} else {
		switch name {
		case "read":
			v.read(item, args, details)
		case "write":
			v.write(item, args, details)
		case "edit":
			v.edit(item, args, details)
		case "bash", "shell", "powershell":
			v.bash(item, args, details)
		case "worker":
			v.worker(item, args, details)
		case "codemode":
			v.codemode(item, args, details)
		case "ask_question":
			v.askQuestion(item, args, details)
		case "web_search":
			v.webSearch(item, args, details)
		case "fetch_content":
			v.fetchContent(item, args, details)
		case "source_check":
			v.sourceCheck(item, args, details)
		case "get_search_content":
			v.getSearchContent(item, args, details)
		case "update_plan":
			v.updatePlan(item, args, details)
		case "web_enable":
			v.webEnable(item, args, details)
		case "obs_recall":
			v.obsRecall(item, args, details)
		default:
			v.generic(item, args, details)
		}
	}
	if len(details) > 0 {
		v.heading("Details")
		v.value(details, "")
	} else if len(item.Details) > 0 && decodeMap(item.Details) == nil && string(item.Details) != "null" {
		v.heading("Details")
		if value, ok := decodeJSON(item.Details); ok {
			v.value(value, "")
		} else {
			v.line(string(item.Details), v.theme.Text)
		}
	}
	for i := range item.Images {
		v.line(fmt.Sprintf("[image attachment %d · open from transcript]", i+1), v.theme.Muted)
	}
	if len(v.rows) == 0 {
		v.line("(waiting for result)", v.theme.Muted)
	}
}
