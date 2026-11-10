package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func TestTranscriptContentSpansExcludeOnlyOuterFrame(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "user"}, {ID: "a", Role: "assistant", Text: "assistant"}}
	body := []string{"  │ genuine box text │", "", "    界é👩‍💻"}
	doc := transcript(items, testTheme(), Options{Width: 60}, func(string, int, config.Theme) []string { return body })
	rows := strings.Split(doc.Text, "\n")
	if len(rows) != len(doc.Content) {
		t.Fatalf("rows=%d spans=%d", len(rows), len(doc.Content))
	}
	var selected []string
	for i, span := range doc.Content {
		if !span.Content {
			continue
		}
		selected = append(selected, ansi.Cut(ansi.Strip(rows[i]), span.Start, span.End))
	}
	want := append(append([]string{}, body...), body...)
	if !reflect.DeepEqual(selected, want) {
		t.Fatalf("content=%q want=%q", selected, want)
	}
	if doc.Content[1].Start <= 2 {
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
		if !reflect.DeepEqual(got.Content, want.Content) {
			t.Fatalf("cached spans differ: %+v / %+v", got.Content, want.Content)
		}
	}
}
