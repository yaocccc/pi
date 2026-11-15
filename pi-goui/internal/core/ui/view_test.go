package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNewSessionSidebarStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		draft   string
		phase   cyclePhase
		working bool
	}{
		{name: "empty"},
		{name: "unsent draft", draft: "not submitted"},
		{name: "explicit submission starting", phase: cycleStarting, working: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 150, Height: 42})
			tab := m.current()
			tab.state.Name = "NEW-LOCAL-SESSION"
			tab.setValue(tc.draft)
			tab.cycle.phase = tc.phase
			if tab.client != nil {
				t.Fatal("new session unexpectedly has a backend")
			}
			var sidebar strings.Builder
			for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
				sidebar.WriteString(ansi.Cut(line, m.mainWidth(), m.width))
				sidebar.WriteByte('\n')
			}
			text := sidebar.String()
			if !strings.Contains(text, "NEW-LOCAL-SESSION") {
				t.Fatalf("session row missing from sidebar: %s", text)
			}
			if strings.Contains(text, "starting") || strings.Contains(text, "idle") == tc.working || strings.Contains(text, "working") != tc.working {
				t.Fatalf("unexpected sidebar status (working=%v): %s", tc.working, text)
			}
			if !tc.working && ansi.Strip(m.workingLine(tab)) != "" {
				t.Fatal("idle belongs in the sidebar, not the working message")
			}
			if tab.client != nil || tab.editor.Value() != tc.draft {
				t.Fatal("rendering started a backend or changed the draft")
			}
		})
	}
}
