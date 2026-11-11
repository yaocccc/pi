package render

import (
	"strings"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// Prefer recorded diffs. Missing old file contents must never be read from the
// current filesystem or presented as a confirmed change to an empty file.
func changeDiffRows(item session.Item, theme config.Theme, width int) []string {
	width = max(1, min(width, changeContentWidth))
	v := toolVisual{theme: theme, width: width}
	line := func(text, color string) {
		for _, row := range strings.Split(Clean(text), "\n") {
			v.rows = append(v.rows, fit(paint(row, color), width))
		}
	}
	if item.Error {
		line("Change failed", theme.Error)
		if strings.TrimSpace(item.Text) != "" {
			line(item.Text, theme.Error)
		}
		return v.rows
	}
	args := visualMap(item.Arguments)
	if visualName(item.ToolName) == "write" {
		status := "writing"
		if item.HasResult && !item.Running {
			status = "written"
		}
		path := visualString(args, "path")
		if path == "" {
			path = "(awaiting path)"
		}
		line("↳ "+status+" "+oneLine(path), theme.Muted)
	}
	if diff := visualString(visualMap(item.Details), "diff", "patch"); diff != "" {
		if item.Running || !item.HasResult {
			line("Diff · completion not confirmed", theme.Muted)
		}
		return append(v.rows, changeGrid(diff, visualString(visualMap(item.Arguments), "path"), width, theme, true)...)
	}
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
			v.rows = append(v.rows, writtenGrid(content, visualString(args, "path"), width, theme)...)
		}
	} else {
		edits := visualEdits(args)
		if len(edits) > 0 {
			line("Replacement preview · not a recorded diff", theme.Muted)
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
		v.rows = append(v.rows, changeGrid(strings.Join(lines, "\n"), visualString(args, "path"), width, theme, false)...)
	}
	if len(v.rows) == 0 {
		line("(no recorded diff)", theme.Muted)
	}
	return v.rows
}
