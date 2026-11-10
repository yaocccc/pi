package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/input"
)

func TestInputCursorTracksCaret(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tab := m.current()
	tab.setValue("中文ab")
	m.resizeEditor(tab)
	m.focus(tab)
	c := m.View().Cursor
	if c == nil || c.X != 8 || c.Y != m.editorTop(tab)+1 {
		t.Fatalf("initial cursor: %+v", c)
	}
	press(m, tea.KeyLeft, 0, "")
	c = m.View().Cursor
	if c == nil || c.X != 7 {
		t.Fatalf("moved cursor: %+v", c)
	}
	// Exercise soft wrapping and a scrolled multiline viewport.
	tab.setValue("第一行\nsecond\nthird\nfourth\nfifth\nsixth\nseventh")
	m.resizeEditor(tab)
	c = m.View().Cursor
	local := tab.editor.Area.Cursor()
	if c == nil || local == nil || c.X != local.X+2 || c.Y != local.Y+m.editorTop(tab)+1 || c.Y >= m.height-2 {
		t.Fatalf("scrolled cursor: %+v, local: %+v", c, local)
	}
}

func TestInputCursorAfterNewlineWrapAndSessionSwitch(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	first := m.current()
	first.setValue("中文")
	m.resizeEditor(first)
	m.focus(first)
	press(m, 'j', tea.ModCtrl, "")
	c := m.View().Cursor
	if c == nil || c.X != 2 || c.Y != m.editorTop(first)+2 {
		t.Fatalf("newline cursor: %+v", c)
	}
	first.setValue("中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文中文")
	m.resizeEditor(first)
	before := m.View().Cursor
	if before == nil {
		t.Fatal("wrapped cursor missing")
	}
	second := m.addTab("")
	second.setValue("ab")
	m.activate(1)
	c = m.View().Cursor
	if c == nil || c.X != 4 || c.Y != m.editorTop(second)+1 {
		t.Fatalf("switched cursor: %+v", c)
	}
	m.activate(0)
	c = m.View().Cursor
	if c == nil || c.X != before.X || c.Y != before.Y {
		t.Fatalf("restored cursor: %+v, want %+v", c, before)
	}
}

func TestPopupCursor(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tab := m.current()
	d := &dialog{kind: "input", title: "Name", message: "Enter a name", editor: input.New()}
	d.editor.SetValue("中文")
	tab.dialogs = []*dialog{d}
	m.resizeDialog(d)
	m.focus(tab)
	p := m.popupGeometry(d)
	c := m.View().Cursor
	if c == nil || c.X != p.x+2+4 || c.Y != p.y+2+len(p.message) {
		t.Fatalf("popup cursor: %+v", c)
	}
	d.kind = "confirm"
	m.resizeDialog(d)
	if c := m.View().Cursor; c != nil {
		t.Fatalf("non-editable popup has cursor: %+v", c)
	}
}
