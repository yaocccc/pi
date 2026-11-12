package render

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Match the working message's 15 shared 80ms ticks (1.2s including its pause).
const todoSweepPeriod = 15

// animateTodoText uses the working message's sweep rules with a lower,
// neutral-gray highlight. The icon, number and task share the same sweep.
func animateTodoText(content string, frame int) string {
	plain := ansi.Strip(content)
	width := ansi.StringWidth(plain)
	phase := (frame % todoSweepPeriod) * 20 / 15
	center := -2 + float64(width+3)*float64(phase)/16
	var out strings.Builder
	var state byte
	column := 0
	for rest := plain; len(rest) > 0; {
		seq, cells, n, next := ansi.DecodeSequence(rest, state, nil)
		rest, state = rest[n:], next
		gray := 136 // #888888 at rest; never tint toward the theme accent.
		if phase < 17 {
			distance := (float64(column) - center) / 2.0
			blend := math.Exp(-0.5*distance*distance) * 0.9
			if blend > 0.01 {
				gray += int(math.Round((190 - 136) * blend))
			}
		}
		fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%dm%s\x1b[39m", gray, gray, gray, seq)
		column += cells
	}
	return out.String()
}
