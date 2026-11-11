package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/input"
	"testing"
)

func TestPointerCoversPopup(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	tab := m.current()
	tab.footerHover = footerHoverState{kind: "model", active: "model"}
	box, _, _ := m.footerPopup(tab, "model")
	for _, point := range [][2]int{{box.x, box.y}, {box.x + 1, box.y + 1}, {box.x + box.width - 1, box.y + box.height - 1}} {
		if got := m.pointerShapeAt(point[0], point[1]); got != "pointer" {
			t.Fatalf("footer popup at %v: %s", point, got)
		}
	}
	tab.footerHover = footerHoverState{}
	d := &dialog{kind: "input", title: "Name", editor: input.New()}
	tab.dialogs = []*dialog{d}
	m.resizeDialog(d)
	p := m.popupGeometry(d)
	for _, point := range [][2]int{{p.x, p.y}, {p.x + 2, p.y + 2}, {p.x + p.width - 1, p.y + p.height - 1}} {
		if got := m.pointerShapeAt(point[0], point[1]); got != "pointer" {
			t.Fatalf("dialog at %v: %s", point, got)
		}
	}
	if got := m.pointerShapeAt(p.x-1, p.y); got != "text" {
		t.Fatalf("outside dialog: %s", got)
	}
}

func TestPointerShapeAndTransitions(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	x, y := 2, m.editorTop(tab)+1
	if got := m.pointerShapeAt(x, y); got != "text" {
		t.Fatalf("input pointer: %s", got)
	}
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = x, y
	if m.updatePointer() == nil {
		t.Fatal("missing pointer update")
	}
	if m.updatePointer() != nil {
		t.Fatal("unchanged pointer emitted again")
	}
	m.sidebarHistory.pointerX = -1
	if m.updatePointer() != nil || m.pointerShape != "text" {
		t.Fatal("non-clickable area should retain text pointer")
	}
	layout := m.sidebarLayout()
	if m.sidebarWidth() > 0 {
		if got := m.pointerShapeAt(m.mainWidth()+2, layout.DirectoryRow); got != "pointer" {
			t.Fatalf("directory pointer: %s", got)
		}
		m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = m.mainWidth()+2, layout.DirectoryRow
		if m.updatePointer() == nil || m.pointerShape != "pointer" {
			t.Fatal("missing transition to pointer")
		}
		m.sidebarHistory.pointerX = -1
		if m.updatePointer() == nil || m.pointerShape != "text" {
			t.Fatal("missing transition back to text")
		}
	}
}
