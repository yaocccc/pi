package ui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

// Task activity uses the shared folding/drilldown helpers. The production
// inherited-history boundary is covered by TestWorkerConversationTaskMessages.
func conversationFixture(t *testing.T) (*Model, *workerSession) {
	t.Helper()
	m, p, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openWorkerSession(p, "w", "index:batch:0"))
	w := m.inspector.worker
	args, _ := json.Marshal(map[string]string{"path": "large.go", "content": "FIRST_SOURCE\n" + strings.Repeat("var x = \"中文 👩‍💻\"\n", 100) + "LAST_SOURCE"})
	w.state.Items = []session.Item{
		{ID: "u", Role: "user", Text: "user message"},
		{ID: "a", Role: "assistant", Text: "assistant **message**", Thinking: "PRIVATE_THINKING"},
		{ID: "write", Role: "tool", ToolName: "write", Arguments: args, HasResult: true},
		{ID: "read", Role: "tool", ToolName: "read", Text: "FIRST_RESULT\n" + strings.Repeat("middle\n", 100) + "LAST_RESULT", HasResult: true},
		{ID: "nested", Role: "tool", ToolName: "worker", Details: json.RawMessage(`{"kind":"worker-ui","batchId":"nested","tasks":[{"index":0,"title":"Nested task","mode":"implement","preset":"deep","status":"completed"}]}`), HasResult: true},
	}
	w.state.Busy = false
	w.visible = w.state.Items
	w.revision++
	m.inspectorRows()
	return m, w
}
func clickConversationRow(t *testing.T, m *Model, row int) {
	t.Helper()
	m.inspector.offset = min(row, max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
	left, _ := m.inspectorBounds()
	v := tea.Mouse{Button: tea.MouseLeft, X: left + m.inspectorInset() + 1, Y: row - m.inspector.offset + 2}
	m.mouse(v)
	m.mouseRelease(v)
	m.inspectorRows()
}
func TestConversationUsesMainDocumentAndFolds(t *testing.T) {
	m, w := conversationFixture(t)
	parent := m.current()
	before, _ := json.Marshal(parent.state)
	check := func() {
		want := render.Transcript(w.state.Items, m.cfg.Theme, conversationOptions(w.state, m.inspector.cache.width, w.expanded, w.showThinking, w.folds))
		if !reflect.DeepEqual(w.doc, want) {
			t.Fatal("inspector does not use ordinary transcript document")
		}
	}
	check()
	var group render.Block
	for _, b := range w.doc.Blocks {
		if b.Role == "toolGroup" {
			group = b
			break
		}
	}
	if group.ID == "" || group.Expanded {
		t.Fatal("calls not initially collapsed")
	}
	clickConversationRow(t, m, group.Start)
	if !w.folds[group.ID] {
		t.Fatal("calls click did not expand")
	}
	check()
	for _, id := range []string{"write", "read", "nested"} {
		found := false
		for _, b := range w.doc.Blocks {
			if b.ID == id {
				clickConversationRow(t, m, b.Start)
				found = true
				break
			}
		}
		if !found || !w.folds[id] {
			t.Fatalf("tool %s did not expand", id)
		}
		check()
	}
	// Body clicks use the parent's fold range; internal worker task cards stay fixed.
	for _, b := range w.doc.Blocks {
		if b.InspectorTask != "" {
			clickConversationRow(t, m, b.Start+1)
			break
		}
	}
	if w.folds["nested"] {
		t.Fatal("worker body did not collapse enclosing batch")
	}
	after, _ := json.Marshal(parent.state)
	if string(before) != string(after) || len(parent.folds) != 0 {
		t.Fatal("parent mutated")
	}
}
func TestConversationDetailDrilldownBackAndCopy(t *testing.T) {
	m, w := conversationFixture(t)
	w.expanded = true
	w.revision++
	m.inspectorRows()
	saved := w.doc.Text
	m.sidebarHistory.pointerKnown = true
	var target conversationTarget
	for _, candidate := range w.targets {
		if candidate.id == "write" {
			target = candidate
			break
		}
	}
	if target.id == "" {
		t.Fatal("missing full-result target")
	}
	m.inspector.offset = min(target.rect.y, max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
	left, _ := m.inspectorBounds()
	v := tea.Mouse{Button: tea.MouseLeft, X: left + m.inspectorInset() + target.rect.x, Y: 2 + target.rect.y - m.inspector.offset}
	m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = v.X, v.Y
	m.mouse(v)
	m.mouseRelease(v)
	if m.inspector.parent == nil || m.inspector.worker != nil || m.inspector.itemID != "write" {
		t.Fatal("did not drill down")
	}
	all := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	if !strings.Contains(all, "FIRST_SOURCE") || !strings.Contains(all, "LAST_SOURCE") {
		t.Fatal("full source unavailable")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.inspector.worker != w || m.inspector.parent != nil || w.doc.Text != saved || !w.expanded {
		t.Fatal("back lost conversation state")
	}
	// A selection never toggles a block, and copies only Content spans.
	for _, b := range w.doc.Blocks {
		if b.ID == "write" {
			m.inspector.offset = min(b.Start, max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			v = tea.Mouse{Button: tea.MouseLeft, X: left + m.inspectorInset() + 2, Y: 2 + b.Start - m.inspector.offset}
			revision := w.revision
			m.mouse(v)
			m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: v.X + 3, Y: v.Y})
			m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: v.X + 3, Y: v.Y})
			if w.revision != revision {
				t.Fatal("selection folded block")
			}
			break
		}
	}
}
func TestConversationStreamInvalidationAndUnknownEvents(t *testing.T) {
	m, p, _ := workerPageFixture(t)
	receiveWorkerCommand(t, m, m.openWorkerSession(p, "w", "index:batch:0"))
	w := m.inspector.worker
	deliver := func(raw string) {
		m.receiveWorkerTranscript(workerTranscriptMsg{owner: w, batch: session.WorkerTranscriptBatch{Records: []json.RawMessage{json.RawMessage(raw)}}})
		m.inspectorRows()
	}
	deliver(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":1}}`)
	deliver(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"FIRST_STREAM"}}`)
	if !strings.Contains(ansi.Strip(w.doc.Text), "FIRST_STREAM") {
		t.Fatal("delta not rendered")
	}
	rows := m.inspector.rows
	revision := w.revision
	deliver(`{"type":"turn_end"}`)
	if revision != w.revision || &rows[0] != &m.inspector.rows[0] {
		t.Fatal("nonvisual event rebuilt transcript")
	}
	deliver(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":" NEXT"}}`)
	if !strings.Contains(ansi.Strip(w.doc.Text), "NEXT") {
		t.Fatal("next delta stale")
	}
}
func TestConversationThinkingKeysAndNestedReturn(t *testing.T) {
	m, w := conversationFixture(t)
	parent := m.current()
	oldThinking, oldExpanded := parent.showThinking, parent.expanded
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX = m.mainWidth() + 3
	m.sidebarHistory.pointerY = 5
	m.key(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m.inspectorRows()
	if w.showThinking || parent.showThinking != oldThinking || strings.Contains(ansi.Strip(w.doc.Text), "PRIVATE_THINKING") {
		t.Fatal("thinking key not isolated")
	}
	m.key(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	m.inspectorRows()
	if !w.expanded || parent.expanded != oldExpanded {
		t.Fatal("tool expansion key not isolated")
	}
	for i := range w.state.Items {
		if w.state.Items[i].ID == "nested" {
			w.state.Items[i].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"nested","tasks":[{"index":0,"title":"Nested task","status":"running","transcript":{"version":1,"id":"` + workerPageID + `"}}]}`)
		}
	}
	w.revision++
	m.inspectorRows()
	var id string
	for _, b := range w.doc.Blocks {
		if b.InspectorTask != "" {
			id = b.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no nested task block")
	}
	w.next = time.Now().Add(time.Hour)
	cmd := m.openConversationDetail(id)
	child := m.inspector.worker
	if child == nil || child == w || m.inspector.parent == nil {
		t.Fatal("nested task not opened")
	}
	m.backInspector()
	if m.inspector.worker != w {
		t.Fatal("nested return lost parent")
	}
	if cmd != nil {
		m.receiveWorkerTranscript(cmd().(workerTranscriptMsg))
	}
	if child.pending == false {
		t.Fatal("closed child accepted a stale read")
	}
	if m.inspector.worker != w || len(m.tabs) != 1 {
		t.Fatal("nested viewer altered tab")
	}
}

func BenchmarkWorkerConversationBatch(b *testing.B) {
	for _, count := range []int{200, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			m := resizeBenchmarkModel(b, "worker", 1)
			w := &workerSession{revision: 1, showThinking: true}
			m.inspector.worker = w
			var entries []map[string]any
			for i := 0; i < count; i++ {
				parentID := ""
				if i > 0 {
					parentID = fmt.Sprint(i - 1)
				}
				entries = append(entries, map[string]any{"type": "message", "id": fmt.Sprint(i), "parentId": parentID, "message": map[string]any{"role": "assistant", "timestamp": i + 1, "content": []map[string]string{{"type": "text", "text": "A short completed message 中文"}}}})
			}
			raw, _ := json.Marshal(map[string]any{"entries": entries})
			if err := w.state.LoadEntries(raw); err != nil {
				b.Fatal(err)
			}
			if len(w.state.Items) != count {
				b.Fatalf("fixture has %d items", len(w.state.Items))
			}
			w.inheritedMessages = w.state.MessageCount
			w.refreshVisibleMessages()
			m.inspectorRows()
			records := []json.RawMessage{json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":999999}}`)}
			for i := 0; i < 100; i++ {
				records = append(records, json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"delta "}}`))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.receiveWorkerTranscript(workerTranscriptMsg{owner: w, batch: session.WorkerTranscriptBatch{Records: records}})
				m.View()
				w.next = time.Time{}
			}
		})
	}
}
