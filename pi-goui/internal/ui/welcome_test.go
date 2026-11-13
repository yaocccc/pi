package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWelcomeShowsEffectiveShortcuts(t *testing.T) {
	m := testModel(t, `{"app.model.select":"alt+m","app.thinking.cycle":"alt+t"}`)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	text := m.welcomeText()
	for _, want := range []string{"alt+m model", "alt+t thinking", "/hotkeys more"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "unbound") {
		t.Fatalf("unknown shortcut action: %s", text)
	}
}

func TestWelcomeAndFooterDefaultHints(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if !strings.Contains(m.welcomeText(), "^L model  ·  Shift+Tab thinking  ·  /hotkeys more") {
		t.Fatal("missing compact welcome shortcuts")
	}
	footer := ansi.Strip(m.inputFooter(m.current()).text)
	if strings.Contains(footer, "stop") || strings.Contains(footer, "quit") {
		t.Fatalf("unexpected footer: %s", footer)
	}
}

func TestWelcomeTitleUsesBrightText(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	plain := strings.Split(m.welcomeText(), "\n")
	styled := strings.Split(m.welcomeView(), "\n")
	if styled[1] != m.paint(plain[1], m.cfg.Theme.Text) {
		t.Fatal("title should use the bright text color")
	}
	if styled[3] != m.paint(plain[3], m.cfg.Theme.Muted) {
		t.Fatal("description should remain muted")
	}
}

func TestWelcomeWrapsAndScrolls(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 10})
	tab := m.current()
	m.document(tab)
	for _, row := range strings.Split(tab.doc.Text, "\n") {
		if ansi.StringWidth(row) > m.mainWidth() {
			t.Fatalf("welcome overflow: %q", row)
		}
	}
	if tab.offset != 0 {
		t.Fatal("welcome should start at the top")
	}
	m.scroll(tab, 1000)
	if tab.offset == 0 {
		t.Fatal("long welcome cannot scroll")
	}
	if len(tab.state.Items) != 0 {
		t.Fatal("welcome leaked into conversation history")
	}
}
