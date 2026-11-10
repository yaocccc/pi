package render

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func assertCachedTranscript(t *testing.T, cache *TranscriptCache, items []session.Item, theme config.Theme, opts Options) Document {
	t.Helper()
	got, want := cache.Transcript(items, theme, opts), Transcript(items, theme, opts)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cached transcript differs\ngot: %#v\nwant: %#v", got, want)
	}
	return got
}

func TestTranscriptCacheMatchesPresentationChanges(t *testing.T) {
	items := []session.Item{
		{ID: "u", Role: "user", Text: "**Question** 中文", MessageIndex: 1},
		{ID: "a", Role: "assistant", Text: "# Answer\n\n```go\nfunc f() {}\n```", Thinking: "private reasoning", MessageIndex: 2},
		{ID: "t1", Role: "tool", ToolID: "t1", ToolName: "bash", Arguments: json.RawMessage(`{"command":"ls"}`), Text: "output", Running: true, MessageIndex: 3},
		{ID: "t2", Role: "tool", ToolID: "t2", ToolName: "read", Text: "second result", MessageIndex: 4},
		{ID: "thinking", Role: "thinking", Text: "hidden", MessageIndex: 5},
		{ID: "system", Role: "system", Text: "Notice"},
		{ID: "unknown", Role: "custom", Text: "fallback"},
		{ID: "u2", Role: "user", Text: "Next", MessageIndex: 6},
		{ID: "a2", Role: "assistant", Text: "Stream", MessageIndex: 7},
	}
	var cache TranscriptCache
	theme := testTheme()
	opts := Options{Width: 80, Expanded: map[string]bool{}}
	check := func() {
		assertCachedTranscript(t, &cache, items, theme, opts)
		assertCachedTranscript(t, &cache, items, theme, opts)
	}
	check()
	items[len(items)-1].Text += " more **text**"
	check()
	items[2].Running = false
	items[2].HasResult = true
	items[2].EstimatedTokens = 42
	check()
	// Raw messages may be mutated in place; saved keys must not alias them.
	copy(items[2].Arguments, `{"command":"ps"}`)
	check()
	items[2].Details = json.RawMessage(`{"x":1}`)
	check()
	copy(items[2].Details, `{"x":2}`)
	check()
	items[2].Error = true
	check()
	opts.Expanded["tool-group:t1"] = true
	check()
	opts.Expanded["t1"] = true
	check()
	opts.ExpandTools = true
	check()
	opts.ShowThinking = true
	check()
	opts.AssistantTitle = "New model"
	check()
	opts.MessageCount = 50
	check()
	theme.Text = "#ff0000"
	theme.AssistantBackground = "#001122"
	check()
	for _, width := range []int{1, 7, 8, 20, 40, 132, 0, 80} {
		opts.Width = width
		check()
	}
	items[1].MessageIndex = 25
	check()
	items = append(items[:1], items[2:]...)
	check()
	items = items[:2]
	check()
	items = nil
	check()
}

func TestTranscriptCachePreservesHiddenAndEmptySections(t *testing.T) {
	for _, width := range []int{1, 8, 80} {
		var cache TranscriptCache
		opts := Options{Width: width}
		cases := [][]session.Item{
			{{Role: "assistant", Thinking: "hidden", MessageIndex: 8}},
			{{Role: "user"}, {Role: "system"}, {Role: "assistant"}, {Role: "notice"}},
			{{Role: "assistant", Text: "one"}, {Role: "thinking", Text: "hidden"}, {Role: "assistant", Text: "two"}},
			{{Role: "assistant", Thinking: "hidden", Error: true}, {Role: "user", Text: "two"}},
		}
		for _, items := range cases {
			assertCachedTranscript(t, &cache, items, testTheme(), opts)
		}
	}
}

func TestTranscriptCacheReusesHistoryAndDiscardsStreamingVersions(t *testing.T) {
	var cache TranscriptCache
	opts := Options{Width: 80}
	items := []session.Item{
		{ID: "u", Role: "user", Text: "Question"},
		{ID: "a", Role: "assistant", Text: "Historical **answer**"},
		{ID: "u2", Role: "user", Text: "Continue"},
		{ID: "a2", Role: "assistant", Text: "Stable part of active bubble"},
		{ID: "a3", Role: "assistant", Text: "Stream"},
	}
	doc := cache.Transcript(items, testTheme(), opts)
	history := &cache.sections[1].doc.Blocks[0]
	key := markdownKey{items[3].Text, 76}
	stable := &cache.sections[3].markdown[key][0]
	// Returned navigation blocks must not expose mutable cache storage.
	doc.Blocks[0].Start = 999
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	for i := 0; i < 30; i++ {
		items[4].Text = fmt.Sprintf("Streaming version %d", i)
		assertCachedTranscript(t, &cache, items, testTheme(), opts)
		if history != &cache.sections[1].doc.Blocks[0] {
			t.Fatal("unchanged history was rendered again")
		}
		if stable != &cache.sections[3].markdown[key][0] {
			t.Fatal("stable Markdown in active bubble was rendered again")
		}
		if len(cache.sections) != 4 || len(cache.sections[3].markdown) != 2 {
			t.Fatal("old streaming versions retained")
		}
	}
	opts.MessageCount = 99
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	if history != &cache.sections[1].doc.Blocks[0] {
		t.Fatal("metadata-only change invalidated history")
	}
	items = items[:1]
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	if len(cache.sections) != 1 {
		t.Fatal("removed sections retained")
	}
	cache.Transcript(nil, testTheme(), opts)
	if len(cache.sections) != 0 {
		t.Fatal("cache retained a previous session")
	}
}

func BenchmarkTranscriptStreaming(b *testing.B) {
	body := strings.Repeat("This is a representative assistant paragraph with **bold text**, `inline code`, and implementation details.\n\n", 8) + "```go\nfunc example() string {\n return \"hello\"\n}\n```\n"
	for _, grouped := range []bool{false, true} {
		layout := "turns"
		if grouped {
			layout = "single-group"
		}
		for _, n := range []int{50, 100} {
			for _, cached := range []bool{false, true} {
				mode := "uncached"
				if cached {
					mode = "cached"
				}
				b.Run(fmt.Sprintf("%s/%d/%s", layout, n, mode), func(b *testing.B) {
					var items []session.Item
					for i := 0; i < n; i++ {
						if !grouped {
							items = append(items, session.Item{ID: fmt.Sprintf("u%d", i), Role: "user", Text: "Please continue."})
						}
						items = append(items, session.Item{ID: fmt.Sprintf("a%d", i), Role: "assistant", Text: fmt.Sprintf("Message %d\n\n", i) + body})
					}
					theme, opts := testTheme(), Options{Width: 100}
					var cache TranscriptCache
					cache.Transcript(items, theme, opts)
					b.ReportAllocs()
					i := 0
					for b.Loop() {
						items[len(items)-1].Text = body + fmt.Sprintf("\nStreaming revision %d", i)
						i++
						if cached {
							cache.Transcript(items, theme, opts)
						} else {
							Transcript(items, theme, opts)
						}
					}
				})
			}
		}
	}
}
