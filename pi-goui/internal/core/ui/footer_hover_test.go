package ui

import (
	"encoding/json"
	"errors"
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

func TestIdleFooterHoverIsLocalAndExplicitClickStartsOperation(t *testing.T) {
	for _, kind := range []string{"model", "thinking"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
			owner := m.current()
			footer := m.inputFooter(owner)
			rect, op := footer.model, "get_available_models"
			if kind == "thinking" {
				rect, op = footer.thinking, "get_available_thinking_levels"
			}
			m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y})
			box, _, _ := m.footerPopup(owner, kind)
			m.Update(tea.MouseMotionMsg{X: box.x + 1, Y: box.y + 1})
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "Click label to open") || strings.Contains(view, "Loading…") || len(owner.footerHover.options) != 0 {
				t.Fatal("new, disconnected tab should offer to open a menu, not invent or load options")
			}
			if owner.cycle.phase != cycleIdle || owner.cycle.intent != nil || owner.client != nil || owner.cycle.inflight != 0 {
				t.Fatal("footer hover started Pi")
			}
			_, cmd := m.Update(tea.MouseClickMsg{X: rect.x, Y: rect.y, Button: tea.MouseLeft})
			if cmd == nil || owner.cycle.phase != cycleStarting || owner.cycle.intent == nil || owner.client != nil {
				t.Fatal("explicit idle selection did not capture its operation")
			}
			owner.client, _ = fakeClient(t)
			reply := commandResult(owner.cycle.intent.run()).(replyMsg)
			if reply.op != op || reply.err != nil {
				t.Fatalf("click requested %s instead of %s: %v", reply.op, op, reply.err)
			}
			m.Update(reply)
			if len(owner.dialogs) != 1 || owner.dialogs[0].kind != kind || len(owner.dialogs[0].options) != 2 {
				t.Fatal("explicit click should open the requested menu with options from Pi")
			}
		})
	}
}

func TestFooterLookupSwitchAndRetry(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	tab := m.current()
	tab.client, _ = fakeClient(t)
	footer := m.inputFooter(tab)
	m.Update(tea.MouseMotionMsg{X: footer.model.x, Y: footer.model.y})
	m.Update(tea.MouseMotionMsg{X: footer.thinking.x, Y: footer.thinking.y})
	if tab.cycle.inflight != 1 || !strings.Contains(ansi.Strip(m.View().Content), "Loading…") {
		t.Fatal("loading must correspond to one pending lookup")
	}
	reply := footerReplyMsg{kind: "model", reply: replyMsg{id: tab.id, client: tab.client, err: errors.New("offline")}}
	cmd := m.footerReply(reply)
	if cmd == nil || !tab.footerHover.loading || tab.footerHover.kind != "thinking" || tab.cycle.inflight != 1 {
		t.Fatal("failed model lookup did not service current thinking control")
	}
	reply = cmd().(footerReplyMsg)
	reply.reply.data = json.RawMessage(`{`)
	if m.footerReply(reply) != nil || m.hoverFooter() != nil || tab.cycle.inflight != 0 || !strings.Contains(ansi.Strip(m.View().Content), "Re-hover to retry") {
		t.Fatal("invalid payload should offer a retry without repeatedly fetching during hover")
	}
	m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	m.Update(tea.MouseMotionMsg{X: footer.thinking.x, Y: footer.thinking.y})
	if !tab.footerHover.loading || tab.cycle.inflight != 1 {
		t.Fatal("returning to the control did not retry the failed lookup")
	}
	reply.reply.data = json.RawMessage(`{"levels":["high"]}`)
	if m.footerReply(reply) != nil || tab.cycle.inflight != 0 || len(tab.footerHover.options) != 1 || tab.footerHover.options[0].Value != "high" {
		t.Fatal("retry did not display backend options")
	}

	// An idle handoff must not present the previous backend's options,
	// including when an old reply arrives while the pointer is stationary.
	m.cycleClosed(cycleClosedMsg{id: tab.id, client: tab.client, generation: tab.cycle.generation})
	m.footerReply(reply)
	if tab.footerHover.loading || len(tab.footerHover.options) != 0 || !strings.Contains(ansi.Strip(m.View().Content), "Click label to open") {
		t.Fatal("backend handoff leaked stale options or left a spinner")
	}
}
