package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	frames "pi-goui/internal/core/render/frame"
	"pi-goui/internal/core/render/tooldetail"
	"pi-goui/internal/core/session"
)

// ToolResultRows is the unabridged, display-only inspector view. It deliberately
// bypasses transcript snippets, tail limits and single-line code truncation.
func ToolResultRows(item session.Item, theme config.Theme, width int, providers ...*ToolViews) []string {
	var provider tooldetail.Provider
	if tools := firstTools(providers); tools != nil {
		provider = tools.FullDetail
	}
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
	}, provider)
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
