package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	frames "pi-goui/internal/render/frame"
	"pi-goui/internal/session"
)

func TestRunningHeadingGradientMovesWithoutLayoutChanges(t *testing.T) {
	theme := testTheme()
	row := paint("│", theme.Border) + " › 3 calls " + paint("│", theme.Border)
	first := AnimateToolActivity(row, theme, 0)
	next := AnimateToolActivity(row, theme, 3)
	if first == next {
		t.Fatal("heading gradient does not move")
	}
	for _, value := range []string{first, next} {
		if ansi.Strip(value) != ansi.Strip(row) || ansi.StringWidth(value) != ansi.StringWidth(row) {
			t.Fatal("animation changed text or geometry")
		}
		if strings.Contains(value, "[48;") || strings.Contains(value, "[0m") {
			t.Fatal("animation changed background or reset styles")
		}
	}
	if AnimateToolActivity("Run…", theme, 2) != "Run…" {
		t.Fatal("clipped heading changed")
	}
}

func activityTestColors(row string) [][3]int {
	var result [][3]int
	var color [3]int
	var state byte
	for len(row) > 0 {
		seq, width, n, next := ansi.DecodeSequence(row, state, nil)
		state, row = next, row[n:]
		if width == 0 {
			var r, g, b int
			if count, _ := fmt.Sscanf(seq, "\x1b[38;2;%d;%d;%dm", &r, &g, &b); count == 3 {
				color = [3]int{r, g, b}
			}
		} else {
			for range width {
				result = append(result, color)
			}
		}
	}
	return result
}

func TestRunningHighlightPreservesBaseColorsAndIncludesSuffix(t *testing.T) {
	theme := testTheme()
	row := paint("› 3 calls", theme.Text) + paint(" · 2edit 1bash", theme.Muted) + paint(" 1❌", theme.Error) + paint(" ≈11.2k", theme.Warning)
	base := activityTestColors(row)
	changedTail := false
	for frame := 0; frame < 15; frame++ {
		value := AnimateToolActivity(row, theme, frame)
		if ansi.Strip(value) != ansi.Strip(row) {
			t.Fatal("animation changed text")
		}
		colors := activityTestColors(value)
		if len(colors) != len(base) {
			t.Fatal("animation changed geometry")
		}
		for i, c := range colors {
			for channel := range c {
				if c[channel] < base[i][channel] {
					t.Fatal("animation dimmed original foreground")
				}
			}
			if i >= 9 && c != base[i] {
				changedTail = true
			}
		}
	}
	if !changedTail {
		t.Fatal("tool counts, failures and token suffix never highlighted")
	}
	for _, frame := range []int{13, 14} {
		if AnimateToolActivity(row, theme, frame) != row {
			t.Fatal("pause must preserve original ANSI colors exactly")
		}
	}
}

func TestActiveCallsMarkerUsesToolRunningColor(t *testing.T) {
	items := []session.Item{{ID: "t", Role: "tool", ToolName: "read", Running: true}}
	for _, expanded := range []bool{false, true} {
		row := toolGroupHeader(items, expanded, testTheme(), 80, true)
		first := AnimateToolActivity(row, testTheme(), 1)
		second := AnimateToolActivity(row, testTheme(), 2)
		arrow := "›"
		if expanded {
			arrow = "⌄"
		}
		if !strings.HasPrefix(ansi.Strip(first), "⋯ 1 calls") || !strings.HasPrefix(ansi.Strip(second), "⋯ 1 calls") {
			t.Fatal("running marker did not replace fold arrow")
		}
		if ansi.StringWidth(first) != ansi.StringWidth(row) || ansi.StringWidth(second) != ansi.StringWidth(row) {
			t.Fatal("spinner changed layout")
		}
		if !strings.Contains(first, paint("⋯", testTheme().Warning)) || !strings.Contains(second, paint("⋯", testTheme().Warning)) {
			t.Fatal("marker must retain running tool color")
		}
		idle := ansi.Strip(toolGroupHeader(items, expanded, testTheme(), 80))
		if !strings.HasPrefix(idle, arrow+" 1 calls") {
			t.Fatal("idle group retained spinner")
		}
	}
}

func TestWorkingGradientMatchesInputPalette(t *testing.T) {
	for _, preset := range []string{"", "catppuccin", "tokyo-night", "nord", "rose-pine"} {
		theme := testTheme()
		theme.FramePreset = preset
		const text = "2 turns · ↑35k ↓1k · 28s"
		row := AnimateWorkingLine(text, theme, 13) // Pause exposes the base gradient.
		if ansi.Strip(row) != text {
			t.Fatal("working gradient changed text")
		}
		colors := activityTestColors(row)
		palette, _ := frames.Palette(preset, "input")
		if colors[0] != palette[0] || colors[len(colors)-1] != palette[1] {
			t.Fatalf("%s working gradient does not match input border", preset)
		}
		if row == AnimateWorkingLine(text, theme, 5) {
			t.Fatal("working highlight stopped moving")
		}
	}
}

func TestActivityHeadingRowsMatchCachedAndUncachedLayout(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "start"}, {ID: "a", Role: "assistant", Text: "Checking"}, {ID: "t", Role: "tool", ToolName: "read", Running: true}}
	for _, width := range []int{20, 80} {
		opts := Options{Width: width, ShowToolActivity: true}
		var cache TranscriptCache
		for _, doc := range []Document{Transcript(items, testTheme(), opts), cache.Transcript(items, testTheme(), opts)} {
			if len(doc.ActivityRows) != 1 {
				t.Fatalf("missing heading metadata: %+v", doc.ActivityRows)
			}
			rows := strings.Split(doc.Text, "\n")
			if !strings.Contains(ansi.Strip(rows[doc.ActivityRows[0]]), "⋯ 1 calls") {
				t.Fatal("heading metadata points to wrong row")
			}
		}
	}
}
