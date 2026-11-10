package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNativeToolHistoryReachesSemanticViews(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	tab := m.current()
	raw := `{"entries":[
 {"id":"a","type":"message","parentId":null,"message":{"role":"assistant","content":[
 {"type":"toolCall","id":"r","name":"read","arguments":{"path":"main.go","offset":7}},
 {"type":"toolCall","id":"e","name":"edit","arguments":{"path":"main.go","edits":[{"oldText":"OLD_REQUEST","newText":"NEW_REQUEST"}]}},
 {"type":"toolCall","id":"w","name":"write","arguments":{"path":"out.ts","content":"export const saved = 42;\n"}},
 {"type":"toolCall","id":"u","name":"custom.inspect","arguments":{"criteria":{"enabled":true},"tags":["one","two"]}}]}},
 {"id":"r","type":"message","parentId":"a","message":{"role":"toolResult","toolCallId":"r","toolName":"read","content":[{"type":"text","text":"package main\nfunc main() {}\n"}]}},
 {"id":"e","type":"message","parentId":"r","message":{"role":"toolResult","toolCallId":"e","toolName":"edit","content":[{"type":"text","text":"Edit complete"}],"details":{"diff":"-10 const before = 1;\n+10 const after = 2;","firstChangedLine":10}}},
 {"id":"w","type":"message","parentId":"e","message":{"role":"toolResult","toolCallId":"w","toolName":"write","content":[{"type":"text","text":"Write complete"}]}},
 {"id":"u","type":"message","parentId":"w","message":{"role":"toolResult","toolCallId":"u","toolName":"custom.inspect","content":[{"type":"text","text":"{\"status\":\"ok\",\"records\":[{\"name\":\"record-one\"}]}"}]}}
 ],"leafId":"u"}`
	if err := tab.state.LoadEntries(json.RawMessage(raw)); err != nil {
		t.Fatal(err)
	}
	tab.expanded = true
	m.document(tab)
	text := ansi.Strip(tab.doc.Text)
	for _, want := range []string{"4 calls", "main.go (Offset: 7)", "7 │ package main", "-10 const before = 1;", "+10 const after = 2;", "Write preview · original content unavailable", "export const saved = 42;", "Enabled: yes", "record-one", "tok"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in semantic history:\n%s", want, text)
		}
	}
	for _, bad := range []string{`"oldText":`, `"criteria":`, `{"status":`, "OLD_REQUEST", "NEW_REQUEST"} {
		if strings.Contains(text, bad) {
			t.Fatalf("protocol/fallback leaked despite authoritative data: %q", bad)
		}
	}
}
func TestSidebarCardClickWheelAndResize(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	for i := 0; i < 14; i++ {
		m.addTab("")
	}
	m.activate(0)
	m.sidebarOffset = 0
	for _, row := range []int{m.sidebarLayout().SessionsStart + 2, m.sidebarLayout().SessionsStart + 3} {
		m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + 3, Y: row})
		if m.active != 1 {
			t.Fatalf("both card rows must activate session 2, row=%d active=%d", row, m.active)
		}
	}
	m.current().dirty = false
	m.current().offset = 7
	oldActive := m.active
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: m.mainWidth() + 3, Y: m.sidebarLayout().SessionsStart})
	if m.sidebarOffset != 3 || m.active != oldActive || m.current().offset != 7 {
		t.Fatalf("sidebar wheel leaked into transcript/focus: offset=%d active=%d chat=%d", m.sidebarOffset, m.active, m.current().offset)
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + 3, Y: m.sidebarLayout().SessionsStart + 1})
	if m.active != 3 {
		t.Fatal("scrolled card click did not map offset")
	}
	m.activate(len(m.tabs) - 1)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	visible := max(1, m.sidebarLayout().Capacity)
	if m.active < m.sidebarOffset || m.active >= m.sidebarOffset+visible {
		t.Fatal("resize hid selected session")
	}
	before := m.active
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + 3, Y: m.height - 1})
	if m.active != before {
		t.Fatal("bottom border activated a session")
	}
}
