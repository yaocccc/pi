package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSessionNavigationKeys(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.setValue("first draft")
	second := m.addTab("")
	second.setValue("second draft")
	press(m, tea.KeyUp, tea.ModCtrl, "")
	if m.current() != first {
		t.Fatal("Ctrl+Up did not select previous session")
	}
	press(m, tea.KeyUp, tea.ModCtrl, "")
	if m.current() != second {
		t.Fatal("previous session did not wrap")
	}
	press(m, tea.KeyDown, tea.ModCtrl, "")
	if m.current() != first {
		t.Fatal("next session did not wrap")
	}
	press(m, tea.KeyDown, tea.ModCtrl, "")
	if m.current() != second {
		t.Fatal("Ctrl+Down did not select next session")
	}
	if first.editor.Value() != "first draft" || second.editor.Value() != "second draft" {
		t.Fatal("navigation changed drafts")
	}
	press(m, 'n', tea.ModCtrl, "")
	if len(m.tabs) != 3 || m.current() == second || m.current().editor.Value() != "" {
		t.Fatal("Ctrl+N did not create a blank session")
	}
}
