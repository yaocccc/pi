package render

import (
	"reflect"
	"testing"

	"pi-goui/internal/core/session"
)

func TestToolGroupsIgnoreOnlyInvisibleRecords(t *testing.T) {
	for _, tc := range []struct {
		name         string
		separator    session.Item
		showThinking bool
		grouped      bool
	}{
		{"empty assistant", session.Item{Role: "assistant"}, false, true},
		{"empty assistant with thinking enabled", session.Item{Role: "assistant"}, true, true},
		{"whitespace", session.Item{Role: "assistant", Text: " \n\t"}, false, true},
		{"hidden thinking", session.Item{Role: "assistant", Thinking: "reasoning"}, false, true},
		{"hidden thinking record", session.Item{Role: "thinking", Text: "reasoning"}, false, true},
		{"visible thinking", session.Item{Role: "assistant", Thinking: "reasoning"}, true, false},
		{"visible text", session.Item{Role: "assistant", Text: "Next step"}, false, false},
		{"error", session.Item{Role: "assistant", Error: true}, false, false},
		{"user", session.Item{Role: "user", Text: "continue"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := []session.Item{
				{ID: "t1", Role: "tool", ToolName: "read", MessageIndex: 1},
				tc.separator,
				{ID: "t2", Role: "tool", ToolName: "bash", MessageIndex: 3},
			}
			var cache TranscriptCache
			for _, expanded := range []bool{false, true} {
				opts := Options{Width: 80, ShowThinking: tc.showThinking, Expanded: map[string]bool{"tool-group:t1": expanded}}
				doc := Transcript(items, testTheme(), opts)
				cached := cache.Transcript(items, testTheme(), opts)
				if !reflect.DeepEqual(doc, cached) {
					t.Fatal("cached and uncached layouts differ")
				}
				groups := 0
				for _, b := range doc.Blocks {
					if b.Role == "toolGroup" {
						groups++
						if b.ID == "tool-group:t1" {
							if b.Expanded != expanded {
								t.Fatalf("lost group state: %+v", b)
							}
						} else if tc.grouped || b.ID != "tool-group:t2" || b.Expanded {
							t.Fatalf("unexpected group: %+v", b)
						}
					}
				}
				want := 2
				if tc.grouped {
					want = 1
				}
				if groups != want {
					t.Fatalf("groups=%d, want grouped=%v", groups, tc.grouped)
				}
			}
		})
	}
}
