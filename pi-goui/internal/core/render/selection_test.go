package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func TestTranscriptContentSpansExcludeOnlyOuterFrame(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "user"}, {ID: "a", Role: "assistant", Text: "assistant"}}
	body := []string{"  │ genuine box text │", "", "    界é👩‍💻"}
	doc := transcript(items, testTheme(), Options{Width: 60}, func(string, int, config.Theme) []string { return body })
	rows := strings.Split(doc.FullText(), "\n")
	if len(rows) != len(allSpans(doc)) {
		t.Fatalf("rows=%d spans=%d", len(rows), len(allSpans(doc)))
	}
	var selected []string
	for i, span := range allSpans(doc) {
		if !span.Content {
			continue
		}
		selected = append(selected, ansi.Cut(ansi.Strip(rows[i]), span.Start, span.End))
	}
	want := append(append([]string{}, body...), body...)
	if !reflect.DeepEqual(selected, want) {
		t.Fatalf("content=%q want=%q", selected, want)
	}
	if allSpans(doc)[1].Start <= 2 {
		t.Fatal("right-aligned user padding not represented")
	}
}

func TestTranscriptCachePreservesContentSpans(t *testing.T) {
	var cache TranscriptCache
	items := []session.Item{{ID: "u", Role: "user", Text: "hello"}, {ID: "a", Role: "assistant", Text: "world"}}
	opts := Options{Width: 60}
	for i := 0; i < 2; i++ {
		got := cache.Transcript(items, testTheme(), opts)
		want := Transcript(items, testTheme(), opts)
		if !reflect.DeepEqual(allSpans(got), allSpans(want)) {
			t.Fatalf("cached spans differ: %+v / %+v", allSpans(got), allSpans(want))
		}
	}
}

func allSpans(d Document) []ContentSpan {
	var out []ContentSpan
	for i := 0; i < d.LineCount(); i++ {
		if v, ok := d.Span(i); ok {
			out = append(out, v)
		}
	}
	return out
}
