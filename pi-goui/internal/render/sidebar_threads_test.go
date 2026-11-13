package render

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSidebarThreadIndentationBounded(t *testing.T) {
	if got := ansi.Strip(sidebarHistoryRow("child", 30, false, 2)); !strings.Contains(got, "  ↳ child") {
		t.Fatalf("lost indentation: %q", got)
	}
	for width := 0; width <= 32; width++ {
		for _, depth := range []int{0, 1, 4, 5000} {
			got := sidebarHistoryRow("a long child title", width, true, depth)
			if ansi.StringWidth(got) > width {
				t.Fatalf("width %d depth %d: %q", width, depth, got)
			}
		}
	}
}
