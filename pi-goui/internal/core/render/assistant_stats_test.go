package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestAssistantStatsInTitleAndCache(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "hello"}, {ID: "a", Role: "assistant", Text: "answer"}}
	opts := Options{Width: 120, AssistantTitle: "Model", AssistantStats: map[string]string{}}
	var cache TranscriptCache
	before := cache.Transcript(items, testTheme(), opts)
	stats := "↑35k ↓1k · 28s"
	opts.AssistantStats["a"] = stats
	doc := cache.Transcript(items, testTheme(), opts)
	if doc.Text == before.Text {
		t.Fatal("new metrics did not invalidate cache")
	}
	for _, row := range strings.Split(ansi.Strip(doc.Text), "\n") {
		if strings.Contains(row, stats) && !strings.Contains(row, "╭─ Model · "+stats) {
			t.Fatal("metrics are not in the title")
		}
	}
	if strings.Count(ansi.Strip(doc.Text), stats) != 1 {
		t.Fatal("missing or duplicated metrics")
	}
	if doc.Text != Transcript(items, testTheme(), opts).Text {
		t.Fatal("cached rendering differs")
	}
	opts.AssistantStats["a"] = "↑40k ↓2k · 40s"
	if cache.Transcript(items, testTheme(), opts).Text == doc.Text {
		t.Fatal("in-place metrics update was cached")
	}
	for _, width := range []int{1, 8, 20, 40, 120} {
		opts.Width = width
		checkWidth(t, cache.Transcript(items, testTheme(), opts).Text, width)
	}
}
