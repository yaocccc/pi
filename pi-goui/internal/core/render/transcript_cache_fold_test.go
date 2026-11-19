package render

import (
	"fmt"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func foldHistory(turns int) []session.Item {
	var items []session.Item
	for i := range turns {
		n := fmt.Sprint(i)
		items = append(items,
			session.Item{ID: "u" + n, Role: "user", Text: "Question " + n},
			session.Item{ID: "a" + n, Role: "assistant", Text: strings.Repeat("**Markdown** `code` 中文 text.\n\n", 8)},
			session.Item{ID: "r" + n, Role: "tool", ToolName: "read", Text: strings.Repeat("line\n", 12)},
			session.Item{ID: "b" + n, Role: "tool", ToolName: "bash", Text: "output"})
	}
	return items
}

func BenchmarkTranscriptCacheFold(b *testing.B) {
	items := foldHistory(128)
	theme := testTheme()
	for _, id := range []string{"tool-group:r64", "b64"} {
		b.Run(id, func(b *testing.B) {
			opts := Options{Width: 96, Expanded: map[string]bool{"tool-group:r64": true}}
			var cache TranscriptCache
			cache.Transcript(items, theme, opts)
			b.ReportAllocs()
			for b.Loop() {
				opts.Expanded[id] = !opts.Expanded[id]
				cache.Transcript(items, theme, opts)
			}
		})
	}
}

func TestTranscriptCacheFoldChangesOnlyOwningSection(t *testing.T) {
	items := foldHistory(6)
	theme := testTheme()
	opts := Options{Width: 80, Expanded: map[string]bool{}, ShowToolActivity: true}
	var c TranscriptCache
	assertCachedTranscript(t, &c, items, theme, opts)
	change := func(id string, section int, value bool, unset bool) {
		t.Helper()
		var blocks []*Block
		for _, s := range c.sections {
			blocks = append(blocks, &s.doc.Blocks[0])
		}
		if unset {
			delete(opts.Expanded, id)
		} else {
			opts.Expanded[id] = value
		}
		assertCachedTranscript(t, &c, items, theme, opts)
		for i, s := range c.sections {
			if i != section && &s.doc.Blocks[0] != blocks[i] {
				t.Fatalf("%s re-rendered unrelated section %d", id, i)
			}
		}
	}
	change("tool-group:r3", 7, true, false)
	change("r3", 7, true, false)
	change("b3", 7, true, false)
	change("r3", 7, false, false)
	change("tool-group:r3", 7, false, true)
	// A hidden child's override must still take effect on the next group open.
	change("b3", 7, false, true)
	change("tool-group:r3", 7, true, false)
	// A recent child is visible before its group opens (activity preview).
	change("b5", 11, true, false)
	change("b5", 11, false, true)
}
