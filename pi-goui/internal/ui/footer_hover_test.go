package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterModelsGroupedByProvider(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	tab := m.current()
	tab.footerHover = footerHoverState{kind: "model", active: "model"}
	m.footerReply(footerReplyMsg{kind: "model", reply: replyMsg{id: tab.id, data: json.RawMessage(`{"models":[{"id":"b1","provider":"beta"},{"id":"a1","provider":"alpha"},{"id":"b2","provider":"beta"}]}`)}})
	want := []string{"alpha", "a1", "beta", "b1", "b2"}
	if len(tab.footerHover.options) != len(want) {
		t.Fatal("missing provider headings")
	}
	for i, label := range want {
		if tab.footerHover.options[i].Label != label {
			t.Fatalf("row %d: want %s", i, label)
		}
	}
	rect := m.inputFooter(tab).model
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y})
	box, _, _ := m.footerPopup(tab, "model")
	m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 1})
	if cmd, handled := m.clickFooterPopup(tab, box.x+2, box.y+1); !handled || cmd != nil {
		t.Fatal("provider heading must not submit a setting")
	}
}

func TestFooterHoverClickSelection(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	tab := m.current()
	c, _ := fakeClient(t)
	tab.client = c
	tab.state.Thinking = "high"
	tab.footerHover = footerHoverState{kind: "thinking", options: []option{{Label: "low", Value: "low"}, {Label: "high", Value: "high"}}}
	rect := m.inputFooter(tab).thinking
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y})
	if !strings.Contains(ansi.Strip(m.View().Content), "› high") {
		t.Fatal("missing current option")
	}
	m.Update(tea.MouseWheelMsg{X: rect.x, Y: rect.y, Button: tea.MouseWheelDown})
	if tab.footerHover.setting || tab.state.Thinking != "high" {
		t.Fatal("wheel changed setting")
	}
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y + 1})
	if !strings.Contains(ansi.Strip(m.View().Content), "Click to set") {
		t.Fatal("popup disappeared inside editor")
	}
	box, _, _ := m.footerPopup(tab, "thinking")
	if box.y+box.height != rect.y || box.y+box.height <= m.editorTop(tab) {
		t.Fatal("popup should extend into editor and anchor above footer")
	}
	beforeHover := m.View().Content
	m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 1})
	if m.View().Content == beforeHover {
		t.Fatal("hover did not highlight option")
	}
	if m.footerHoverKind(tab) != "thinking" {
		t.Fatal("popup disappeared on entry")
	}
	m.Update(tea.MouseWheelMsg{X: box.x + 2, Y: box.y + 1, Button: tea.MouseWheelDown})
	if tab.footerHover.setting {
		t.Fatal("popup wheel changed setting")
	}
	cmd := m.mouse(tea.Mouse{X: box.x + 2, Y: box.y + 1, Button: tea.MouseLeft})
	if cmd == nil {
		t.Fatal("option not clickable")
	}
	if m.footerHoverKind(tab) != "" {
		t.Fatal("popup remained open after selection")
	}
	reply := commandResult(cmd).(footerReplyMsg)
	if reply.reply.op != "set_thinking_level" || reply.selected.Value != "low" {
		t.Fatalf("wrong selection: %+v", reply)
	}
	m.Update(tea.MouseMotionMsg{X: box.x - 1, Y: rect.y + 1})
	if strings.Contains(ansi.Strip(m.View().Content), "Click to set") {
		t.Fatal("popup remained after moving left within editor")
	}
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y + 1})
	if m.footerHoverKind(tab) != "" {
		t.Fatal("closed popup reopened without hovering a label")
	}
}
