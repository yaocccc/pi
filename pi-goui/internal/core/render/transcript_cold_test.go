package render

import (
	"fmt"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

// Compare the whole document, including private link coordinates, block spans,
// styles, selections and numbering against the ordinary serial renderer.
func TestColdManyTurnDocumentEquivalence(t *testing.T) {
	var items []session.Item
	forms := []string{"[a](https://example.org/x)", "```go\nvar n = 3\n```", "| H | V |\n|---|---|\n| hi | [v](https://example.org) |", "你好 🙂 **bold**\n\n> note", "[ref]: https://example.org/hidden", "[alias][ref]", "<b>text</b> &amp;", "\x1b[2K\x1b]0;unsafe\aabc"}
	for i := range 70 {
		items = append(items, session.Item{ID: fmt.Sprintf("u%d", i), Role: "user", Text: forms[i%len(forms)]}, session.Item{ID: fmt.Sprintf("a%d", i), Role: "assistant", Text: fmt.Sprintf("%s\n\nAnswer %d.", forms[(i+1)%len(forms)], i), MessageIndex: i + 1})
	}
	items = append(items, session.Item{ID: "tool", Role: "tool", ToolName: "read", Text: "details"})
	for _, width := range []int{19, 88} {
		opts := Options{Width: width, ExpandTools: true}
		theme := testTheme()
		var cache TranscriptCache
		got := cache.Transcript(items, theme, opts)
		want := Transcript(items, theme, opts)
		if !equalDocuments(got, want) {
			t.Fatalf("serial and cold document differ at width %d", width)
		}
	}
}

func TestColdMarkdownSkipsHugeBodiesAndDeduplicates(t *testing.T) {
	const text = "same"
	items := []session.Item{{Role: "assistant", Text: strings.Repeat("a", (64<<10)+1)}, {Role: "user", Text: text}, {Role: "user", Text: text}, {Role: "assistant", Text: text}, {Role: "tool", Text: "t"}}
	work := prepareColdMarkdown(items, 80, testTheme())
	if len(work) != 2 || work[markdownKey{text, 60}] == nil || work[markdownKey{text, 76}] == nil {
		t.Fatalf("preparation should be scoped to small, distinct (text,width) pairs")
	}
}

// Isolate a cold layout after native history has been decoded. Distinct message
// bodies rule out savings from memoizing a repeated fixture paragraph.
func BenchmarkColdManyTurnTranscript(b *testing.B) {
	var items []session.Item
	for i := range 600 {
		items = append(items, session.Item{ID: fmt.Sprint("q", i), Role: "user", Text: fmt.Sprintf("Question %d: please explain the next change.", i)}, session.Item{ID: fmt.Sprint("a", i), Role: "assistant", Text: fmt.Sprintf("Step %d: update **the adapter** to call `dispatch`.\n\n- Verify the status.\n- Run the unit tests.", i)})
	}
	theme, opts := testTheme(), Options{Width: 92, MessageCount: len(items)}
	b.ReportAllocs()
	for b.Loop() {
		var c TranscriptCache
		_ = c.Transcript(items, theme, opts)
	}
}
