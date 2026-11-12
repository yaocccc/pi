package render

import (
	frames "pi-goui/internal/render/frame"
	"strings"
	"testing"

	"fmt"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func gradientCorner(kind, glyph string) string {
	corners, _ := frames.Palette("pi", kind)
	c := corners[0]
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[39m", c[0], c[1], c[2], glyph)
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
