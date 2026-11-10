package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestToolGroupHasNoAutomaticPreviews(t *testing.T) {
	items := []session.Item{
		{ID: "a", Role: "tool", ToolName: "first", HasResult: true, Text: "short summary\nhidden output"},
		{ID: "b", Role: "tool", ToolName: "second", HasResult: true, Text: "short summary\nhidden output"},
		{ID: "c", Role: "tool", ToolName: "third", HasResult: true, Text: "short summary\nhidden output"},
		{ID: "d", Role: "tool", ToolName: "fourth", Running: true},
	}
	opts := Options{Width: 100}
	doc := Transcript(items, testTheme(), opts)
	group := blockByID(t, doc, "tool-group:a")
	if group.Expanded || group.End-group.Start != 1 {
		t.Fatalf("expected a collapsed single-line header: %+v", group)
	}
	plain := ansi.Strip(doc.Text)
	for _, want := range []string{"› 4 calls"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %s: %s", want, plain)
		}
	}
	if strings.Contains(plain, "✓ first") || strings.Contains(plain, "✓ second") || strings.Contains(plain, "✓ third") || strings.Contains(plain, "⋯ fourth") || strings.Contains(plain, "……") || strings.Contains(plain, "hidden output") {
		t.Fatal("preview exposed extra tools or results")
	}
	items[3].Running = false
	items[3].HasResult = true
	settled := Transcript(items, testTheme(), opts)
	if b := blockByID(t, settled, "tool-group:a"); b.Expanded || b.End-b.Start != 1 {
		t.Fatal("tool completion changed the collapsed header")
	}
	for _, global := range []bool{false, true} {
		collapsed := Transcript(items, testTheme(), Options{Width: 100, ExpandTools: global, Expanded: map[string]bool{"tool-group:a": false}})
		b := blockByID(t, collapsed, "tool-group:a")
		if b.Expanded || b.End-b.Start != 1 || strings.Contains(ansi.Strip(collapsed.Text), "✓ second") {
			t.Fatalf("explicit collapse must hide every preview: %+v\n%s", b, collapsed.Text)
		}
	}
	opened := Transcript(items, testTheme(), Options{Width: 100, Expanded: map[string]bool{"tool-group:a": true}})
	if !blockByID(t, opened, "tool-group:a").Expanded || !strings.Contains(ansi.Strip(opened.Text), "✓ first") {
		t.Fatal("explicit expansion must show all calls")
	}
	items = append(items, session.Item{ID: "answer", Role: "assistant", Text: "Done."})
	done := Transcript(items, testTheme(), opts)
	group = blockByID(t, done, "tool-group:a")
	if group.Expanded || group.End-group.Start != 1 {
		t.Fatalf("completed group did not collapse: %+v", group)
	}
}
