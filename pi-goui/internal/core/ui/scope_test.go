package ui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

func TestHistoryOnlyAtVisualBoundary(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	draft := strings.Repeat("wrapped words 界 ", 20)
	tab.setValue(draft)
	tab.history = []string{"old prompt"}
	m.resizeEditor(tab)
	if tab.editor.Area.LineInfo().RowOffset == 0 {
		t.Fatal("fixture did not wrap")
	}
	press(m, tea.KeyUp, 0, "")
	assertValue(t, tab, draft)
	for i := 0; i < 100 && tab.editor.Area.LineInfo().RowOffset > 0; i++ {
		press(m, tea.KeyUp, 0, "")
	}
	press(m, tea.KeyUp, 0, "")
	assertValue(t, tab, "old prompt")
}

func TestDisabledSubmitAndRemappedModalCancel(t *testing.T) {
	m := testModel(t, `{"tui.input.submit":[],"tui.input.newLine":[],"tui.select.cancel":["alt+q"],"tui.altScreen.searchClose":["alt+x"]}`)
	tab := m.current()
	tab.setValue("keep")
	m.search = "previous"
	press(m, tea.KeyEnter, 0, "")
	press(m, tea.KeyEnter, tea.ModShift, "")
	assertValue(t, tab, "keep")
	if tab.sending {
		t.Fatal("disabled submit sent a prompt")
	}
	d := m.newDialog("input", "Input", "")
	tab.dialogs = []*dialog{d}
	press(m, tea.KeyEscape, 0, "")
	if len(tab.dialogs) != 1 {
		t.Fatal("global interrupt bypassed remapped dialog cancellation")
	}
	press(m, 'q', tea.ModAlt, "")
	if len(tab.dialogs) != 0 {
		t.Fatal("configured modal cancellation didn't work")
	}
	m.searching = "find"
	press(m, tea.KeyEscape, 0, "")
	if m.searching == "" {
		t.Fatal("select cancellation bypassed search-close remap")
	}
	press(m, 'x', tea.ModAlt, "")
	if m.searching != "" {
		t.Fatal("configured search-close didn't work")
	}
}
