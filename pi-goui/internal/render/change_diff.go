package render

import (
	"strings"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// Prefer recorded diffs. Missing old file contents must never be read from the
// current filesystem or presented as a confirmed change to an empty file.
func changeDiffRows(item session.Item, theme config.Theme, width int) []string {
	v := toolVisual{theme: theme, width: width}
	if item.Error {
		v.line("Change failed", theme.Error)
		if strings.TrimSpace(item.Text) != "" {
			v.line(item.Text, theme.Error)
		}
		return v.rows
	}
	if diff := visualString(visualMap(item.Details), "diff", "patch"); diff != "" {
		if item.Running || !item.HasResult {
			v.line("Diff · completion not confirmed", theme.Muted)
		}
		return append(v.rows, diffRows(diff, width, theme)...)
	}
	args := visualMap(item.Arguments)
	var lines []string
	appendLines := func(prefix, text string) {
		if text == "" {
			return
		}
		for _, line := range strings.Split(strings.TrimSuffix(cleanCode(text), "\n"), "\n") {
			lines = append(lines, prefix+line)
		}
	}
	if visualName(item.ToolName) == "write" {
		if content, ok := args["content"].(string); ok {
			v.line("Write preview · original content unavailable", theme.Muted)
			appendLines("+", content)
			if content == "" {
				v.line("(empty content)", theme.Muted)
			}
		}
	} else {
		edits := visualEdits(args)
		if len(edits) > 0 {
			v.line("Replacement preview · not a recorded diff", theme.Muted)
		}
		for i, edit := range edits {
			if i > 0 {
				lines = append(lines, "...")
			}
			appendLines("-", visualString(edit, "oldText"))
			appendLines("+", visualString(edit, "newText"))
		}
	}
	if len(lines) > 0 {
		v.rows = append(v.rows, diffRows(strings.Join(lines, "\n"), width, theme)...)
	}
	if len(v.rows) == 0 {
		v.line("(no recorded diff)", theme.Muted)
	}
	return v.rows
}
