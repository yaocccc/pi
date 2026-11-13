package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	extdetail "pi-goui/internal/extensions/toolrender/detail"
	frames "pi-goui/internal/render/frame"
	"pi-goui/internal/render/tooldetail"
	"pi-goui/internal/session"
)

// ToolResultRows is the unabridged, display-only inspector view. It deliberately
// bypasses transcript snippets, tail limits and single-line code truncation.
func ToolResultRows(item session.Item, theme config.Theme, width int) []string {
	return tooldetail.Render(item, theme, width, tooldetail.Hooks{
		Line: func(text, color string, width int) []string {
			var rows []string
			// Style source lines separately: multiline styling pads short rows.
			for _, line := range strings.Split(cleanCode(text), "\n") {
				rows = append(rows, visualWrap(paint(line, color), width)...)
			}
			return rows
		},
		Clean: Clean, CodeRows: codeRows, DiffRows: diffRows, DiffCounts: diffCounts,
	})
}

// WorkerTaskResult and WorkerTaskSummaryRows are host compatibility routes.
func WorkerTaskResult(item session.Item, key string) (session.Item, string, bool) {
	return extdetail.WorkerTaskResult(item, key)
}
func WorkerTaskSummaryRows(item session.Item, reason string, theme config.Theme, width int) []string {
	return extdetail.WorkerTaskSummaryRows(item, reason, theme, width, func(text, color string, width int) []string { return visualWrap(paint(text, color), width) }, Clean, ToolResultRows)
}

// ToolResultFrame uses the sidebar's square corners, padding and gradient.
// The caller supplies interior rows, including its title and status rows.
func ToolResultFrame(content []string, width int, theme config.Theme) []string {
	width = max(1, width)
	border := func(left, right string) string {
		if width == 1 {
			return left
		}
		return left + strings.Repeat("─", width-2) + right
	}
	rows := []string{border("┌", "┐")}
	for _, line := range content {
		if width == 1 {
			rows = append(rows, "│")
			continue
		}
		pad := ""
		if width >= 4 {
			pad = " "
		}
		inner := width - 2 - 2*len(pad)
		line = fit(line, inner)
		rows = append(rows, "│"+pad+line+strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))+pad+"│")
	}
	rows = append(rows, border("└", "┘"))
	return frames.Gradient("sidebar", rows, width, theme)
}

func ToolResultTitle(item session.Item) string {
	path := visualString(visualMap(item.Arguments), "path")
	return strings.TrimSpace(oneLine(item.ToolName + " " + path))
}
