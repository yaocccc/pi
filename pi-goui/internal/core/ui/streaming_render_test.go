package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
	rpc "pi-goui/internal/runtime"
)

func streamingModel(t testing.TB, size int) (*Model, []eventMsg) {
	t.Helper()
	root := t.TempDir()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Cwd: root, Bin: filepath.Join(root, "not-real-pi")}, cfg)
	t.Cleanup(func() { _ = m.Close() })
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	tab := m.current()
	body := strings.Repeat("中文 👩‍💻 **streaming** text with `code`.\n\n", size/55+1)
	var events []eventMsg
	for i := 0; i < 2; i++ {
		raw, _ := json.Marshal(map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant", "timestamp": 42, "content": body + fmt.Sprint(i)}})
		events = append(events, eventMsg{id: tab.id, event: rpc.Event{Data: raw}})
	}
	m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_start"}`)}})
	m.Update(events[0])
	m.document(tab)
	return m, events
}

func finishStreamingDocument(m *Model) {
	if job := m.current().documentJob; job != nil {
		m.Update(job.render())
	}
}

func TestStreamingInputDoesNotFlushDocument(t *testing.T) {
	m, events := streamingModel(t, 4096)
	tab := m.current()
	tab.follow = true
	m.clampScroll(tab)
	before := tab.doc.Text
	m.Update(events[1])
	for _, msg := range []tea.Msg{
		tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 3, Y: 5},
		tea.MouseMotionMsg{X: 4, Y: 6},
		tea.KeyPressMsg{Code: 'x', Text: "x"},
		nil, // Cursor blink/unhandled messages use the same default path.
	} {
		m.Update(msg)
		m.View()
		if tab.doc.Text != before || !tab.dirty || tab.documentJob != nil {
			t.Fatalf("%T flushed pending stream content", msg)
		}
	}
	offset := tab.offset
	m.Update(tickMsg(time.Now()))
	job := tab.documentJob
	if job == nil || tab.doc.Text != before {
		t.Fatal("tick did not schedule an isolated layout")
	}
	// More events/ticks must not queue another job or starve this snapshot.
	m.Update(events[0])
	m.Update(tickMsg(time.Now()))
	if tab.documentJob != job {
		t.Fatal("more than one layout in flight")
	}
	m.Update(job.render())
	if tab.doc.Text == before || !tab.dirty || tab.documentJob != nil {
		t.Fatal("compatible older snapshot not published")
	}
	if tab.offset != offset || tab.follow {
		t.Fatal("publication lost the user's scroll position")
	}
	m.Update(tickMsg(time.Now()))
	finishStreamingDocument(m)
	want := render.Transcript(tab.state.Items, m.cfg.Theme, m.documentOptions(tab))
	if tab.dirty || !reflect.DeepEqual(tab.doc, want) {
		t.Fatal("latest layout differs from full render")
	}
	if got := strings.Join(tab.documentLines(), "\n"); got != tab.doc.Text {
		t.Fatal("line index is stale")
	}
}

func TestStreamingLayoutSnapshotAndInvalidation(t *testing.T) {
	for _, change := range []string{"resize", "theme", "fold", "backend", "session", "selection", "panel", "background", "closed"} {
		t.Run(change, func(t *testing.T) {
			m, events := streamingModel(t, 1024)
			tab := m.current()
			m.Update(events[1])
			m.Update(tickMsg(time.Now()))
			job := tab.documentJob
			if job == nil {
				t.Fatal("missing layout")
			}
			before := tab.doc.Text
			switch change {
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 97, Height: 32})
				before = tab.doc.Text
			case "theme":
				m.cfg.Theme.Text = "#aa1122"
				tab.invalidateDocument()
				m.document(tab)
				before = tab.doc.Text
			case "fold":
				tab.folds["tool"] = true
				tab.invalidateDocument()
				m.document(tab)
				before = tab.doc.Text
			case "backend":
				tab.client = new(rpc.Client)
			case "session":
				tab.state.ID = "replacement"
			case "selection":
				tab.selection.dragging = true
			case "panel":
				m.panelResize.dragging = true
			case "background":
				m.addTab("")
				m.document(m.current())
			case "closed":
				m.tabs = nil
			}
			result := job.render().(documentResult)
			m.receiveDocument(result)
			if change == "backend" {
				tab.client = nil
			} // Not a running RPC client.
			if change == "background" {
				if tab.doc.Text != result.doc.Text || m.current().doc.Text == result.doc.Text {
					t.Fatal("result routed to wrong tab")
				}
			} else if tab.doc.Text != before {
				t.Fatal("invalid/frozen layout was published")
			}
		})
	}
}

func TestStreamingSnapshotOwnsMutableInputs(t *testing.T) {
	m, events := streamingModel(t, 1024)
	tab := m.current()
	tab.state.Items = append(tab.state.Items, session.Item{ID: "tool", Role: "tool", ToolName: "read", Arguments: json.RawMessage(`{"path":"before"}`), Details: json.RawMessage(`{"value":1}`), Images: []session.Image{{Data: "before"}}, Compaction: &session.CompactionMetrics{}})
	tab.folds["tool"] = true
	tab.assistantStats = map[string]string{"stat": "before"}
	tab.invalidateStream()
	cmd := m.scheduleDocument(tab)
	job := tab.documentJob
	copy := job.items[len(job.items)-1]
	tab.state.Items[len(tab.state.Items)-1].Arguments[0] = 'x'
	tab.state.Items[len(tab.state.Items)-1].Details[0] = 'x'
	tab.state.Items[len(tab.state.Items)-1].Images[0].Data = "after"
	tab.folds["tool"] = false
	tab.assistantStats["stat"] = "after"
	if copy.Arguments[0] != '{' || copy.Details[0] != '{' || copy.Images[0].Data != "before" || !job.opts.Expanded["tool"] || job.opts.AssistantStats["stat"] != "before" || copy.Compaction == tab.state.Items[len(tab.state.Items)-1].Compaction {
		t.Fatal("snapshot shares mutable inputs")
	}
	// Exercise cache ownership under the race detector: a synchronous resize
	// and incoming data may proceed while the worker renders its own snapshot.
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	m.Update(events[1])
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 35})
	before := tab.doc.Text
	m.Update(<-result)
	if before != tab.doc.Text {
		t.Fatal("old worker replaced resized document")
	}
}

func TestDocumentLinesReuse(t *testing.T) {
	tab := &tab{doc: render.Document{Text: "first\n界👩‍💻\n"}}
	lines := tab.documentLines()
	if len(lines) != 3 || &lines[0] != &tab.documentLines()[0] {
		t.Fatal("published document index was rebuilt")
	}
	if n := testing.AllocsPerRun(100, func() { _ = tab.documentLines() }); n != 0 {
		t.Fatalf("cached lines allocated %v times", n)
	}
	tab.doc.Text = "replacement"
	if got := tab.documentLines(); len(got) != 1 || got[0] != tab.doc.Text {
		t.Fatal("replacement kept stale index")
	}
}

func TestStreamingMarkdownBoundaries(t *testing.T) {
	m, _ := streamingModel(t, 1024)
	tab := m.current()
	for _, body := range []string{
		"中文 👩‍💻\n\n```go\nfunc main() {",
		"中文 👩‍💻\n\n```go\nfunc main() {}\n```\n\n| 列 | Value |\n| --- | --- |",
		"中文 👩‍💻\n\n```go\nfunc main() {}\n```\n\n| 列 | Value |\n| --- | --- |\n| 👩‍💻 | `text` |\n\n> quote\n\n- item\n  - child",
	} {
		raw, _ := json.Marshal(map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant", "timestamp": 42, "content": body}})
		m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: raw}})
		m.Update(tickMsg(time.Now()))
		finishStreamingDocument(m)
		want := render.Transcript(tab.state.Items, m.cfg.Theme, m.documentOptions(tab))
		if !reflect.DeepEqual(tab.doc, want) {
			t.Fatal("streaming Markdown layout/geometry differs from full rendering")
		}
	}
}

func TestStreamingSettledFlushAndFollow(t *testing.T) {
	m, events := streamingModel(t, 1024)
	tab := m.current()
	tab.follow = true
	m.clampScroll(tab)
	m.Update(events[1])
	m.Update(tickMsg(time.Now()))
	job := tab.documentJob
	m.Update(events[0])
	event(m, tab, `{"type":"agent_settled"}`)
	m.Update(job.render())
	if !tab.dirty {
		t.Fatal("settled final state lost behind in-flight layout")
	}
	m.Update(tickMsg(time.Now()))
	finishStreamingDocument(m)
	want := render.Transcript(tab.state.Items, m.cfg.Theme, m.documentOptions(tab))
	if tab.dirty || !reflect.DeepEqual(want, tab.doc) {
		t.Fatal("settled document not final")
	}
	if !tab.follow || tab.offset != max(0, len(tab.documentLines())-m.transcriptHeight(tab)) {
		t.Fatal("bottom follow lost")
	}
}

// Includes event application in the wheel/tick cases; View measures the already
// published document independently. No RPC process or real model is started.
func BenchmarkStreamingInteraction(b *testing.B) {
	for _, size := range []int{4096, 65536} {
		for _, phase := range []string{"event", "view", "wheel", "tick", "layout"} {
			b.Run(fmt.Sprintf("%d/%s", size, phase), func(b *testing.B) {
				m, events := streamingModel(b, size)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if phase == "view" {
						m.View()
						continue
					}
					m.Update(events[(i+1)%2])
					switch phase {
					case "wheel":
						m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 3, Y: 5})
					case "tick":
						m.Update(tickMsg(time.Now()))
						// Measure scheduling, not the worker's layout. Complete it
						// outside timing so each sample starts with no pending job.
						if job := m.current().documentJob; job != nil {
							b.StopTimer()
							m.Update(job.render())
							b.StartTimer()
						}
					case "layout":
						m.document(m.current())
					}
				}
			})
		}
	}
}
