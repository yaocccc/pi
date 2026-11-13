package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func TestInputFooterModelAndThinkingHitRegions(t *testing.T) {
	for _, withGit := range []bool{false, true} {
		m := testModel(t, "")
		c, _ := fakeClient(t)
		tab := m.current()
		tab.client = c
		tab.state.Model = session.ModelInfo{ID: "gpt-6-astra"}
		tab.state.Thinking = "high"
		m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
		if withGit {
			m.gitCwd = m.opts.Cwd
			m.gitInfo = &render.GitStatus{Branch: "main", Modified: 2}
		}
		footer := m.inputFooter(tab)
		cmd := m.mouse(tea.Mouse{Button: tea.MouseLeft, X: footer.model.x, Y: footer.model.y})
		if cmd != nil {
			t.Fatal("model click should not open a selector")
		}
		cmd = m.mouse(tea.Mouse{Button: tea.MouseLeft, X: footer.thinking.x, Y: footer.thinking.y})
		if cmd != nil {
			t.Fatal("thinking click should not change intensity")
		}
		if len(tab.dialogs) != 0 {
			t.Fatal("thinking click opened a selector")
		}
	}
}
