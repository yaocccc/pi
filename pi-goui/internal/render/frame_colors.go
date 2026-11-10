package render

import (
	"fmt"
	"pi-goui/internal/config"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Copied from extensions/ui/utils.ts FRAME_COLORS. Variation is by frame edge,
// not message-body text. System has no source palette, so it shares input purple.
var framePalettes = map[string][8]string{
	"user":      {"#68d3ae", "#6ed8b4", "#79dcbc", "#87dec3", "#7bd4b8", "#6ccdaf", "#5fc6a5", "#62ccaa"},
	"assistant": {"#78b5ef", "#80bff5", "#8ac7f6", "#91c8ef", "#85bbe8", "#77afe0", "#6ea6dc", "#71afe6"},
	"tool":      {"#71899f", "#71899f", "#71899f", "#71899f", "#71899f", "#71899f", "#71899f", "#71899f"},
	"input":     {"#b6a0e8", "#bca8ee", "#c4b0f2", "#c8b5ef", "#bda9e7", "#b19ddd", "#a892d8", "#ad97df"},
}
var frameEdges = map[string]int{"topLeft": 0, "top": 1, "topRight": 2, "right": 3, "bottomRight": 4, "bottom": 5, "bottomLeft": 6, "left": 7}
var frameANSI = func() map[string][8]string {
	result := make(map[string][8]string, len(framePalettes))
	for role, palette := range framePalettes {
		var colors [8]string
		for i, color := range palette {
			rgb, _ := strconv.ParseUint(color[1:], 16, 24)
			colors[i] = fmt.Sprintf("\x1b[38;2;%d;%d;%dm", rgb>>16, (rgb>>8)&255, rgb&255)
		}
		result[role] = colors
	}
	return result
}()

// FrameBorder colors trusted border text with the reference edge palette and
// resets only foreground. Unknown kinds/edges fall back to neutral tool/top.
func FrameBorder(kind, edge, text string) string {
	if text == "" {
		return ""
	}
	if kind == "system" {
		kind = "input"
	}
	colors, ok := frameANSI[kind]
	if !ok {
		colors = frameANSI["tool"]
	}
	index, ok := frameEdges[edge]
	if !ok {
		index = frameEdges["top"]
	}
	return colors[index] + text + "\x1b[39m"
}

// FrameBox makes a square, transparent frame around already-rendered body rows.
// Width includes borders and, where space permits, one padding cell per side.
// It is used by the editor so its text/footer widths and hit positions stay put.
func FrameBox(kind string, body []string, width int, themes ...config.Theme) []string {
	if width <= 0 {
		return nil
	}
	border := func(left, right, edge, leftEdge, rightEdge string) string {
		if width == 1 {
			return FrameBorder(kind, leftEdge, left)
		}
		return FrameBorder(kind, leftEdge, left) + FrameBorder(kind, edge, strings.Repeat("─", width-2)) + FrameBorder(kind, rightEdge, right)
	}
	rows := []string{border("┌", "┐", "top", "topLeft", "topRight")}
	padding, inside := 0, max(0, width-2)
	if width >= 4 {
		padding = 1
		inside = width - 4
	}
	for _, line := range body {
		if width == 1 {
			rows = append(rows, FrameBorder(kind, "left", "│"))
			continue
		}
		line = ansi.Truncate(line, inside, "")
		rows = append(rows, FrameBorder(kind, "left", "│")+strings.Repeat(" ", padding)+line+strings.Repeat(" ", max(0, inside-ansi.StringWidth(line))+padding)+FrameBorder(kind, "right", "│"))
	}
	return gradientFrame(kind, append(rows, border("└", "┘", "bottom", "bottomLeft", "bottomRight")), width, themes...)
}
