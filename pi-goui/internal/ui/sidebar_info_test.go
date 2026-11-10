package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func TestInputFooterModelAndThinkingHitRegions(t *testing.T) {
	for _, withGit := range []bool{false, true} {
		m := testModel(t, "")
		c, wire := fakeClient(t)
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
		if cmd == nil {
			t.Fatal("model region not clickable")
		}
		reply := commandResult(cmd).(replyMsg)
		if reply.op != "get_available_models" {
			t.Fatal(reply.op)
		}
		cmd = m.mouse(tea.Mouse{Button: tea.MouseLeft, X: footer.thinking.x, Y: footer.thinking.y})
		if cmd == nil {
			t.Fatal("thinking region not clickable")
		}
		reply = commandResult(cmd).(replyMsg)
		if reply.op != "cycle_thinking_level" {
			t.Fatal(reply.op)
		}
		records := wireRecords(t, wire, 2)
		if records[1]["type"] != "cycle_thinking_level" {
			t.Fatalf("thinking should cycle without a selector: %v", records)
		}
		if len(tab.dialogs) != 0 {
			t.Fatal("thinking click opened a selector")
		}
	}
}
