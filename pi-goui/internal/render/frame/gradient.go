package frame

import (
	"fmt"
	"pi-goui/internal/config"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Gradient changes only the outer border, before any alignment padding.
// Body ANSI styles, terminal-cell geometry and transparent backgrounds survive.
func Gradient(kind string, rows []string, width int, themes ...config.Theme) []string {
	preset := ""
	if len(themes) > 0 {
		preset = themes[0].FramePreset
	}
	corners, ok := Palette(preset, kind)
	if !ok || width < 1 || len(rows) == 0 {
		return rows
	}
	paintAt := func(text string, x, y int) string {
		var rgb [3]int
		for c := range rgb {
			top := corners[0][c] + (corners[1][c]-corners[0][c])*x/max(1, width-1)
			bottom := corners[2][c] + (corners[3][c]-corners[2][c])*x/max(1, width-1)
			rgb[c] = top + (bottom-top)*y/max(1, len(rows)-1)
		}
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", rgb[0], rgb[1], rgb[2], text)
	}
	for y, row := range rows {
		if y == 0 || y == len(rows)-1 {
			var line, label strings.Builder
			x := 0
			flushLabel := func() {
				if label.Len() > 0 {
					text := label.String()
					line.WriteString(paintAt(text, x, y))
					x += ansi.StringWidth(text)
					label.Reset()
				}
			}
			for _, r := range ansi.Strip(row) {
				if strings.ContainsRune("─╭╮╰╯┌┐└┘", r) {
					flushLabel()
					line.WriteString(paintAt(string(r), x, y))
					x++
				} else {
					// Keep Unicode titles intact, rather than inserting escape
					// sequences inside a grapheme or changing their typography.
					label.WriteRune(r)
				}
			}
			flushLabel()
			rows[y] = line.String()
		} else if width == 1 {
			rows[y] = paintAt("│", 0, y)
		} else {
			rows[y] = paintAt("│", 0, y) + ansi.Cut(row, 1, width-1) + paintAt("│", width-1, y)
		}
	}
	return rows
}
