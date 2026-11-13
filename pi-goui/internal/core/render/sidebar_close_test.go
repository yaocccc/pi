package render

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestSidebarSessionCloseHover(t *testing.T) {
	for _, width := range []int{0, 4, 7, 8, 10, 28} {
		for _, hover := range []bool{false, true} {
			rows := sidebarSessionCards([]SessionRow{{ID: "1", Name: "long session title", Status: "working +3", Active: true, Hovered: hover}}, width, 2)
			start, length := SidebarSessionCloseButton(width)
			if width == 0 && len(rows) == 0 {
				continue
			}
			if len(rows) != 2 {
				t.Fatal("wrong row count")
			}
			first := ansi.Strip(rows[0])
			shown := hover && length > 0
			if strings.Contains(first, "关闭") != shown || strings.Contains(rows[1], "关闭") {
				t.Fatalf("hover=%v width=%d rows=%q", hover, width, rows)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width {
					t.Fatalf("width %d: %q", width, row)
				}
			}
			if shown && ansi.StringWidth(strings.Split(first, "关闭")[0]) != start {
				t.Fatalf("hitbox mismatch: %q", first)
			}
		}
	}
}
