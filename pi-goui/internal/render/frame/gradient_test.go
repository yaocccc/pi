package frame

import (
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"regexp"
	"strings"
	"testing"
)

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
			rows := Box(kind, []string{"content", "body"}, 40, theme)
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
			rows := Box(kind, make([]string, 10), 30)
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
