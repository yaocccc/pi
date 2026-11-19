package ui

import (
	"fmt"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func TestLongInspectorTransitionUsesBackgroundLayout(t *testing.T) {
	m := inspectorFixture(t)
	tab := m.current()
	// One real fixture tool followed by enough distinct blocks for background reflow.
	for i := 0; i < 530; i++ {
		tab.state.Items = append(tab.state.Items, session.Item{ID: fmt.Sprint(i), Role: "user", Text: "history"})
	}
	m.document(tab)
	tab.follow = false
	tab.offset = 0
	published := tab.doc.Text
	m.openInspector("write-1")
	if !m.inspectorOpen() || tab.doc.Text != published || !tab.streamDirty || len(m.inspectorRows()) == 0 {
		t.Fatal("opening blocked on rebuilding the transcript or failed to expose detail")
	}
	old := m.scheduleDocument(tab)
	if old == nil {
		t.Fatal("no pending width layout")
	}
	m.closeInspector()
	if m.inspectorOpen() || tab.doc.Text != published {
		t.Fatal("Esc synchronously reflowed or failed to close")
	}
	m.Update(old())
	if !tab.dirty || tab.doc.Text != published {
		t.Fatal("stale inspector-width layout replaced the transcript")
	}
	current := m.scheduleDocument(tab)
	if current == nil {
		t.Fatal("no replacement close layout")
	}
	m.Update(current())
	if tab.dirty || tab.doc.Text != published {
		t.Fatal("close did not restore the original viewport layout")
	}
	m.openInspector("write-1")
	job := m.scheduleDocument(tab)
	if job == nil {
		t.Fatal("no open layout")
	}
	m.Update(job())
	if tab.dirty || !m.inspectorOpen() || strings.Join(m.inspectorRows(), "") == "" {
		t.Fatal("valid layout/detail not available")
	}
	// Re-opening at the same width must not invalidate a clean publication.
	gen := tab.layoutGeneration
	m.openInspector("write-1")
	if tab.layoutGeneration != gen || tab.dirty {
		t.Fatal("same-width detail switch rebuilt the transcript")
	}
}

func TestInspectorPositionHintCannotSelectReplacementTool(t *testing.T) {
	m := inspectorFixture(t)
	tab := m.current()
	original := tab.state.Items[0]
	m.openInspector(original.ID)
	tab.state.Items = []session.Item{{ID: "other", Role: "tool", Text: "wrong"}, original}
	if it, ok := m.inspectedTool(); !ok || it.ID != original.ID {
		t.Fatal("position hint selected wrong tool after history replacement")
	}
	tab.state.Items = tab.state.Items[:1]
	if m.inspectorOpen() {
		t.Fatal("removed tool remained available")
	}
}

// The pointer is over the final tool in a long, already-published transcript.
// Keep transcript layout out of the timed path: the old scan included every
// historical frame even though their rows could not be hit.
func BenchmarkInspectLastTool(b *testing.B) {
	for _, n := range []int{100, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			cfg, err := config.Load(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			m := New(Options{Cwd: b.TempDir()}, cfg)
			b.Cleanup(func() { _ = m.Close() })
			m.width, m.height = 120, 40
			tab := m.current()
			var rows []string
			for i := 0; i < n; i++ {
				id := fmt.Sprintf("t%d", i)
				tab.state.Items = append(tab.state.Items, session.Item{ID: id, Role: "tool", ToolName: "read"})
				start := len(rows)
				tab.doc.Blocks = append(tab.doc.Blocks, render.Block{ID: id, Role: "tool", Start: start, End: start + 5, Expanded: true, Foldable: true})
				rows = append(rows, "tool", "╭──────────────────╮", "│payload           │", "╰──────────────────╯", "")
			}
			tab.doc.Text = strings.Join(rows, "\n")
			tab.docLines, tab.docLinesText = rows, tab.doc.Text
			tab.offset = len(rows) - 5
			x, y := 16, transcriptTop+2
			want := tab.state.Items[n-1].ID
			if id, _, ok := m.toolInspectTarget(x, y); !ok || id != want {
				b.Fatalf("target=%s want=%s", id, want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.toolInspectTarget(x, y)
			}
		})
	}
}
