package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestToolFailureDoesNotFailOrStopSession(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	tab := m.current()
	client, _ := fakeClient(t)
	tab.client = client
	sidebar := func() string {
		var rows []string
		for _, row := range strings.Split(m.View().Content, "\n") {
			rows = append(rows, ansi.Strip(ansi.Cut(row, m.mainWidth(), m.width)))
		}
		return strings.Join(rows, "\n")
	}
	event(m, tab, `{"type":"agent_start"}`)
	event(m, tab, `{"type":"tool_execution_start","toolCallId":"bad","toolName":"bash"}`)
	event(m, tab, `{"type":"tool_execution_end","toolCallId":"bad","toolName":"bash","isError":true,"result":{"content":[{"type":"text","text":"failed"}]}}`)
	if !tab.state.Busy || tab.dead || !strings.Contains(sidebar(), "working") {
		t.Fatal("tool failure stopped the session")
	}
	event(m, tab, `{"type":"agent_settled"}`)
	if tab.dead || strings.Contains(sidebar(), "error") || !strings.Contains(sidebar(), "idle") {
		t.Fatalf("tool failure marked idle session as error: %s", sidebar())
	}
	if !tab.state.Items[0].Error {
		t.Fatal("tool failure indicator was lost")
	}
	tab.state.Items = []session.Item{{Role: "assistant", Error: true, Text: "API failure"}}
	if !strings.Contains(sidebar(), "error") {
		t.Fatal("assistant failure should still be reported")
	}
	tab.awaitingStart = true
	if !strings.Contains(sidebar(), "working") || strings.Contains(sidebar(), "error") {
		t.Fatal("new prompt retained previous failure status")
	}
}
