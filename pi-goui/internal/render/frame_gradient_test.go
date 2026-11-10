package render

import (
	"regexp"
	"strings"
	"testing"

	"fmt"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func gradientCorner(kind, glyph string) string {
	corners, _ := frameGradient("pi", kind)
	c := corners[0]
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", c[0], c[1], c[2], glyph)
}

func TestThemeFramePresets(t *testing.T) {
	kinds := []string{"user", "assistant", "tool", "worker", "input", "system", "sidebar", "popup", "codemode", "error"}
	seenThemes := map[string]bool{}
	for _, name := range config.ThemeNames() {
		theme, err := config.LoadTheme(t.TempDir(), name)
		if err != nil {
			t.Fatal(err)
		}
		seenRoles := map[string]bool{}
		var signature string
		for _, kind := range kinds {
			rows := FrameBox(kind, []string{"content", "body"}, 40, theme)
			key := strings.Join(rows, "\n")
			if seenRoles[key] {
				t.Fatalf("%s reuses gradient for %s", name, kind)
			}
			seenRoles[key] = true
			signature += key
			for _, row := range rows {
				if ansi.StringWidth(row) != 40 || strings.Contains(row, "[48;") {
					t.Fatal("gradient changed geometry/background")
				}
			}
		}
		if seenThemes[signature] {
			t.Fatalf("%s reuses another theme's presets", name)
		}
		seenThemes[signature] = true
	}
}

func TestFrameGradientVariesAcrossBothAxes(t *testing.T) {
	colors := regexp.MustCompile(`\x1b\[38;2;[0-9;]+m`)
	for _, kind := range []string{"user", "assistant", "system", "input", "sidebar"} {
		t.Run(kind, func(t *testing.T) {
			rows := FrameBox(kind, make([]string, 10), 30)
			top, left := map[string]bool{}, map[string]bool{}
			for _, color := range colors.FindAllString(rows[0], -1) {
				top[color] = true
			}
			for _, row := range rows {
				left[colors.FindString(row)] = true
				if ansi.StringWidth(row) != 30 || strings.Contains(row, "[48;") || strings.Contains(row, "[0m") {
					t.Fatal("gradient changed geometry or introduced a background/reset")
				}
			}
			if len(top) < 20 || len(left) < 8 {
				t.Fatalf("edge colors are not continuous: horizontal=%d vertical=%d", len(top), len(left))
			}
		})
	}
}

func TestFrameGradientPreservesStyledBodyAndUnicodeTitle(t *testing.T) {
	body := "\x1b[48;2;82;85;88m\x1b[1m你好 e\u0301 👩‍💻\x1b[22m\x1b[49m"
	rows, spans := frameContent([]string{body}, "模型 e\u0301 👩‍💻", 32, "assistant", true, 40)
	if !strings.Contains(rows[0], "模型 e\u0301 👩‍💻") || !strings.Contains(rows[1], body) {
		t.Fatal("gradient damaged a Unicode title or body styling")
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 40 || !strings.HasPrefix(row, strings.Repeat(" ", 8)) {
			t.Fatal("right-aligned frame geometry changed")
		}
	}
	if !spans[1].Content || spans[1].Start != 10 {
		t.Fatal("selection geometry changed")
	}
}

func TestSidebarUsesGradientWithoutChangingNarrowGeometry(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 30} {
		for _, height := range []int{1, 2, 20} {
			rows := strings.Split(Sidebar(session.State{}, nil, testTheme(), width, height), "\n")
			if len(rows) != height {
				t.Fatal("sidebar height changed")
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width || !strings.Contains(row, "\x1b[38;2;") {
					t.Fatalf("sidebar gradient/width missing: %dx%d", width, height)
				}
			}
		}
	}
}
