package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"testing"
)

func TestSidebarCloseIdleAndRunning(t *testing.T) {
	for _, running := range []bool{false, true} {
		for _, background := range []bool{false, true} {
			m := testModel(t, "")
			m.width, m.height = 120, 36
			first := m.current()
			second := m.addTab("")
			m.activate(0)
			target := first
			index := 0
			if background {
				target, index = second, 1
			}
			target.state.Busy = running
			target.setValue("preserve draft")
			x := m.width - 3
			y := m.sidebarLayout().SessionsStart + index*render.SidebarSessionRows
			m.sidebarHistory.pointerKnown = true
			m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = x, y
			if !m.sidebarSessionHovered(index) {
				t.Fatal("card not hovered")
			}
			m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			if running {
				if len(m.tabs) != 2 || m.current() != target || len(target.dialogs) != 1 || target.dialogs[0].kind != "close" {
					t.Fatal("running close did not activate and confirm")
				}
				m.dialogKey(target, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
				if len(m.tabs) != 2 || target.editor.Value() != "preserve draft" || !target.state.Busy {
					t.Fatal("cancel changed running tab")
				}
				m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
				target.dialogs[0].selected = 1
				m.confirmDialog(target, target.dialogs[0])
				if len(m.tabs) != 1 || m.byID(target.id) != nil {
					t.Fatal("confirmation did not close running tab")
				}
			} else if len(m.tabs) != 1 || m.byID(target.id) != nil {
				t.Fatal("idle close did not remove tab")
			}
		}
	}
}

func TestSidebarCloseBackgroundPendingDialog(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	target := m.addTab("")
	target.state.Busy = true
	pending := m.newDialog("input", "Task question", "")
	target.dialogs = append(target.dialogs, pending)
	m.activate(0)
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.width - 3, Y: m.sidebarLayout().SessionsStart + render.SidebarSessionRows})
	if m.current() != target || len(target.dialogs) != 2 || target.dialogs[0].kind != "close" {
		t.Fatal("close confirmation hidden behind existing dialog")
	}
	m.dialogKey(target, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
	if len(target.dialogs) != 1 || target.dialogs[0] != pending {
		t.Fatal("cancel lost pending question")
	}
}

func TestSidebarCloseGeometryAndSelection(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	for i := 0; i < 5; i++ {
		m.addTab("")
	}
	m.sidebarOffset = 2
	x, y := m.width-3, m.sidebarLayout().SessionsStart
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = x, y+1
	if !m.sidebarSessionHovered(2) || m.sidebarSessionHovered(0) {
		t.Fatal("hover ignored offset")
	}
	// Lower row at the same x selects, never closes.
	target := m.tabs[2]
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y + 1})
	if len(m.tabs) != 6 || m.current() != target {
		t.Fatal("name row closed instead of selecting")
	}
	m.sidebarHistory.pointerX = m.width - 2
	if m.sidebarSessionHovered(2) {
		t.Fatal("outer padding hovered")
	}
	target.dialogs = append(target.dialogs, m.newDialog("confirm", "modal", ""))
	m.sidebarHistory.pointerX = x
	if m.sidebarSessionHovered(2) {
		t.Fatal("modal exposed close affordance")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
	if len(m.tabs) != 6 {
		t.Fatal("modal allowed sidebar close")
	}
}
