package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestChangeCardsShowOnlyRecordedDiff(t *testing.T) {
	for _, name := range []string{"edit", "write"} {
		item := visualItem(name, map[string]any{"path": "file.go", "content": "not authoritative", "oldText": "before", "newText": "after"}, map[string]any{"diff": "-old\n+new", "firstChangedLine": 1}, "Operation completed")
		plain := ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
		if plain != "-old\n+new" {
			t.Fatalf("%s card contains non-diff content: %s", name, plain)
		}
		for _, width := range []int{40, 120} {
			rows := toolBody(item, testTheme(), Options{ExpandTools: true}, width)
			if ansi.StringWidth(rows[1]) != len(name)+6 {
				t.Fatal("diff card stretches to viewport width")
			}
		}
		for _, width := range []int{1, 2, 4, 8, 20, 80} {
			checkWidth(t, strings.Join(toolBody(item, testTheme(), Options{ExpandTools: true}, width), "\n"), width)
		}
		item.Error = true
		item.Text = "Permission denied"
		plain = ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
		if !strings.Contains(plain, "Permission denied") || strings.Contains(plain, "+new") {
			t.Fatal("failed operation shown as applied diff")
		}
	}
}
