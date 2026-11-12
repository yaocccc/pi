package render

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTodoSweepMatchesWorkingParameters(t *testing.T) {
	if todoSweepPeriod != 15 {
		t.Fatal("todo must share the working message's 15-frame period")
	}
	colors := regexp.MustCompile(`\x1b\[38;2;(\d+);\d+;\d+m`)
	for _, width := range []int{1, 12, 40, 100} {
		content := strings.Repeat("x", width)
		for frame := 0; frame < 30; frame++ {
			// Same phase, travel, sigma, strength and cutoff as animateForeground.
			phase := (frame % 15) * 20 / 15
			center := -2 + float64(width+3)*float64(phase)/16
			for col, rgb := range colors.FindAllStringSubmatch(animateTodoText(content, frame), -1) {
				want := 136
				if phase < 17 {
					distance := (float64(col) - center) / 2.0
					blend := math.Exp(-0.5*distance*distance) * 0.9
					if blend > 0.01 {
						want = int(math.Round(136 + (190-136)*blend))
					}
				}
				level, _ := strconv.Atoi(rgb[1])
				if level != want {
					t.Fatalf("working sweep mismatch: width=%d frame=%d column=%d got=%d want=%d", width, frame, col, level, want)
				}
			}
		}
	}
	// The icon and number participate in the same entering highlight.
	prefix := colors.FindAllStringSubmatch(animateTodoText("○ 1. Running step", 2), -1)
	for _, col := range []int{0, 2} {
		level, _ := strconv.Atoi(prefix[col][1])
		if level <= 136 {
			t.Fatal("icon and number must participate in the sweep")
		}
	}
}

func TestTodoTextNeutralSweep(t *testing.T) {
	colors := regexp.MustCompile(`\x1b\[38;2;(\d+);(\d+);(\d+)m`)
	for _, content := range []string{"", "○ 3. 检查 👩‍💻 é", "○ 12. Verify changes"} {
		previous := ""
		changed := false
		for frame := 0; frame < todoSweepPeriod; frame++ {
			got := animateTodoText(content, frame)
			if ansi.Strip(got) != content || ansi.StringWidth(got) != ansi.StringWidth(content) {
				t.Fatal("animation changed text or width")
			}
			if got != animateTodoText(content, frame+todoSweepPeriod) {
				t.Fatal("sweep must repeat at its configured period")
			}
			for _, rgb := range colors.FindAllStringSubmatch(got, -1) {
				level, _ := strconv.Atoi(rgb[1])
				if rgb[1] != rgb[2] || rgb[2] != rgb[3] || level < 136 || level > 190 {
					t.Fatalf("expected neutral gray to soft white: %v", rgb)
				}
				if (frame%15)*20/15 >= 17 && level != 136 {
					t.Fatal("pause should return to neutral gray")
				}
			}
			if frame > 0 && got != previous {
				changed = true
			}
			previous = got
		}
		if content != "" && !changed {
			t.Fatal("text must animate")
		}
	}
}
