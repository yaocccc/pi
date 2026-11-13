package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestHotkeysScopesAndEffectiveBindings(t *testing.T) {
	m := testModel(t, `{"app.session.new":"alt+n","app.model.select":[],"app.models.save":"alt+s","app.tree.filter.all":"alt+a","app.tools.expand":"ctrl+o"}`)
	text := m.hotkeysText()
	for _, want := range []string{
		"Main input / session", "Transcript navigation", "Transcript search (while searching)",
		"Session selector (/resume)", "Menus / completions", "Text editing (input and editable dialogs)",
		"alt+n  app.session.new", "ctrl+n  app.session.toggleNamedFilter",
		"ctrl+d, ctrl+backspace  app.session.delete", "with confirmation",
		"override editor bindings", "only when input is empty", "accepts a visible completion first",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in help:\n%s", want, text)
		}
	}
	for _, stale := range []string{"app.models.", "app.tree.", "app.tools.expand", "deleteNoninvasive", "tui.editor.page", "tui.editor.history", "app.model.select"} {
		if strings.Contains(text, stale) {
			t.Fatalf("stale/disabled action %q in help", stale)
		}
	}
	selector := strings.Index(text, "Session selector (/resume)")
	menus := strings.Index(text, "Menus / completions")
	if !strings.Contains(text[selector:menus], "app.session.toggleNamedFilter") || strings.Contains(text[:selector], "app.session.toggleNamedFilter") {
		t.Fatal("session filter is not scoped to session selector")
	}
}

func TestHotkeysAliasesAndWrappedScrolling(t *testing.T) {
	for _, name := range []string{"hotkeys", "shortcuts", "help"} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 48, Height: 24})
			tab := m.current()
			tab.setValue("/" + name)
			m.submit(tab, false)
			if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "help" {
				t.Fatal("help dialog did not open")
			}
			d := tab.dialogs[0]
			if d.editor.Value() != m.hotkeysText() {
				t.Fatal("dialog did not use scoped help")
			}
			for _, width := range []int{48, 28} {
				m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
				p := m.popupGeometry(d)
				lines := popupHelpLines(d, p.inner)
				for _, line := range lines {
					if ansi.StringWidth(line) > p.inner {
						t.Fatalf("help row overflows: %q", line)
					}
				}
				for i := 0; i < len(lines); i++ {
					m.key(tea.KeyPressMsg{Code: tea.KeyPgDown})
				}
				p = m.popupGeometry(d)
				if p.start+p.count != len(lines) {
					t.Fatalf("cannot reach end: start=%d count=%d total=%d", p.start, p.count, len(lines))
				}
			}
		})
	}
}
