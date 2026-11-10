package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestMessageFramesTouchWithoutAddedBlankRows(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "你好", MessageIndex: 1}, {ID: "a", Role: "assistant", Text: "Hello", MessageIndex: 2}, {ID: "s", Role: "system", Text: "Context compacted — earlier messages retained\nSummary", MessageIndex: 3}, {ID: "u2", Role: "user", Text: "Next", MessageIndex: 4}}
	doc := Transcript(items, testTheme(), Options{Width: 80, MessageCount: 4})
	for _, line := range strings.Split(ansi.Strip(doc.Text), "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatal("inserted gap between message frames")
		}
	}
	if doc.MessageCount != 4 {
		t.Fatal("lost total")
	}
	for i, b := range doc.Blocks {
		if b.MessageIndex != i+1 {
			t.Fatalf("wrong ordinal: %+v", b)
		}
	}
	// A user paragraph separation remains part of its source, unlike frame gaps.
	paragraph := Transcript([]session.Item{{ID: "p", Role: "assistant", Text: "first\n\nsecond"}}, testTheme(), Options{Width: 80})
	if !strings.Contains(ansi.Strip(paragraph.Text), "first") || !strings.Contains(ansi.Strip(paragraph.Text), "second") {
		t.Fatal("source paragraph lost")
	}
}
func TestCollapsedToolGroupCarriesLastMessageOrdinal(t *testing.T) {
	items := []session.Item{{ID: "t1", Role: "tool", ToolName: "read", Text: "a", MessageIndex: 2}, {ID: "t2", Role: "tool", ToolName: "read", Text: "b", MessageIndex: 3}, {ID: "a", Role: "assistant", Text: "after", MessageIndex: 4}}
	closed := Transcript(items, testTheme(), Options{Width: 80, MessageCount: 4})
	group := blockByID(t, closed, "tool-group:t1")
	if group.MessageIndex != 3 {
		t.Fatal("collapsed members not counted")
	}
	opened := Transcript(items, testTheme(), Options{Width: 80, MessageCount: 4, ExpandTools: true})
	if blockByID(t, opened, "tool-group:t1").MessageIndex != 0 || blockByID(t, opened, "t1").MessageIndex != 2 || blockByID(t, opened, "t2").MessageIndex != 3 {
		t.Fatal("expanded group reported hidden future messages")
	}
}
