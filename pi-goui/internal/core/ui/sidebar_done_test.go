package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	rpc "pi-goui/internal/runtime"
)

func TestSidebarDoneUntilSessionActivated(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	background := m.addTab("")
	m.activate(0)
	background.client = &rpc.Client{}
	m.applySingleEvent(background, rpc.Event{Data: []byte(`{"type":"agent_settled"}`)})
	check := func(want bool) {
		t.Helper()
		if got := strings.Contains(ansi.Strip(m.View().Content), "done +1"); got != want {
			t.Fatalf("sidebar completion visible = %v, want %v", got, want)
		}
	}
	check(true)
	m.cycleClosed(cycleClosedMsg{id: background.id, client: background.client, generation: background.cycle.generation})
	check(true)
	m.activate(1)
	check(false)
	m.activate(0)
	check(false)
}
