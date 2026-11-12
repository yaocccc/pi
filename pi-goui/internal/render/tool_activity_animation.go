package render

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	frames "pi-goui/internal/render/frame"
)

var activityCallLabel = regexp.MustCompile(`[›⌄⋯] [0-9]+ calls`)

// AnimateToolActivity overlays a highlight on the original foreground colors.
// It never dims semantic colors or changes the cached, selectable document.
func AnimateToolActivity(row string, theme config.Theme, frame int) string {
	plain := ansi.Strip(row)
	location := activityCallLabel.FindStringIndex(plain)
	if location == nil {
		return row
	}
	start := ansi.StringWidth(plain[:location[0]]) + 2 // Keep the status marker in its original color.
	end := ansi.StringWidth(strings.TrimRight(plain, " │"))
	return animateForeground(row, theme, frame, start, end, "assistant")
}

// AnimateWorkingLine matches the input frame's palette, with a left-to-right
// gradient and the same gentle highlight sweep, without adding a spinner.
func AnimateWorkingLine(row string, theme config.Theme, frame int) string {
	plain := ansi.Strip(row)
	width := ansi.StringWidth(plain)
	palette, _ := frames.Palette(theme.FramePreset, "input")
	var gradient strings.Builder
	var state byte
	column := 0
	for rest := plain; len(rest) > 0; {
		seq, cells, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		var color [3]int
		for c := range color {
			color[c] = palette[0][c] + (palette[1][c]-palette[0][c])*column/max(1, width-1)
		}
		fmt.Fprintf(&gradient, "\x1b[38;2;%d;%d;%dm%s\x1b[39m", color[0], color[1], color[2], seq)
		column += cells
	}
	return animateForeground(gradient.String(), theme, frame, 0, width, "input")
}

func animateForeground(row string, theme config.Theme, frame, start, end int, kind string) string {
	phase := (frame % 15) * 20 / 15 // 80ms ticks: 1.2s cycle, including a short pause.
	center := -2 + float64(end-start+3)*float64(phase)/16
	palette, _ := frames.Palette(theme.FramePreset, kind)
	baseColor, _ := strconv.ParseUint(strings.TrimPrefix(theme.Text, "#"), 16, 24)
	if len(theme.Text) != 7 {
		baseColor = 0xc8c8cc
	}
	defaultRGB := [3]int{int(baseColor >> 16), int(baseColor >> 8 & 255), int(baseColor & 255)}
	rgb, restore := defaultRGB, "\x1b[39m"
	var out strings.Builder
	var state byte
	column := 0
	for rest := row; len(rest) > 0; {
		seq, width, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		if width == 0 {
			// Track foreground changes while leaving all original ANSI attributes intact.
			if strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m") {
				params := strings.Split(seq[2:len(seq)-1], ";")
				for i := 0; i < len(params); i++ {
					code, _ := strconv.Atoi(params[i])
					switch {
					case code == 0 || code == 39:
						rgb, restore = defaultRGB, "\x1b[39m"
					case code == 38 && i+4 < len(params) && params[i+1] == "2":
						for c := range rgb {
							rgb[c], _ = strconv.Atoi(params[i+2+c])
						}
						restore = "\x1b[" + strings.Join(params[i:i+5], ";") + "m"
						i += 4
					case code == 38 && i+2 < len(params) && params[i+1] == "5":
						index, _ := strconv.Atoi(params[i+2])
						r, g, b, _ := ansi.IndexedColor(index).RGBA()
						rgb = [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
						restore = "\x1b[" + strings.Join(params[i:i+3], ";") + "m"
						i += 2
					case (code == 48 || code == 58) && i+4 < len(params) && params[i+1] == "2":
						i += 4
					case (code == 48 || code == 58) && i+2 < len(params) && params[i+1] == "5":
						i += 2
					case code >= 30 && code <= 37 || code >= 90 && code <= 97:
						index := code - 30
						if code >= 90 {
							index = code - 90 + 8
						}
						r, g, b, _ := ansi.BasicColor(index).RGBA()
						rgb = [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
						restore = fmt.Sprintf("\x1b[%dm", code)
					}
				}
			}
			out.WriteString(seq)
			continue
		}
		if phase < 17 && column >= start && column < end {
			distance := (float64(column-start) - center) / 2.0
			blend := math.Exp(-0.5*distance*distance) * 0.9
			if blend > 0.01 {
				var highlight [3]int
				for c := range highlight {
					// Tint toward a pale assistant accent, but never darken the base color.
					target := math.Max(float64(rgb[c]), float64(palette[1][c])*0.5+127.5)
					highlight[c] = int(math.Round(float64(rgb[c]) + (target-float64(rgb[c]))*blend))
				}
				fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm%s%s", highlight[0], highlight[1], highlight[2], seq, restore)
			} else {
				out.WriteString(seq)
			}
		} else {
			out.WriteString(seq)
		}
		column += width
	}
	return out.String()
}
