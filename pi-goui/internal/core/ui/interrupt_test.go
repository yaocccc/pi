package ui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	rpc "pi-goui/internal/runtime"
)

func TestEscapeAbortsActiveSessionBeforeCompletionsAndSearch(t *testing.T) {
	for _, state := range []string{"streaming", "tool", "compacting", "sending", "preflight"} {
		t.Run(state, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			client, wire := fakeClient(t)
			tab.client = client
			switch state {
			case "streaming", "tool":
				tab.state.Busy = true
			case "compacting":
				tab.state.Compacting = true
			case "sending":
				tab.sending = true
			case "preflight":
				tab.awaitingStart = true
			}
			tab.state.Steering = []string{"keep queue"}
			tab.setValue("/cpt")
			m.searching = "find"
			m.search = "query"
			reply := commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEscape})).(replyMsg)
			if reply.op != "abort" || !tab.interruptRequested || !m.completionHidden || m.searching != "" {
				t.Fatalf("did not interrupt %s: %+v", state, reply)
			}
			records := wireRecords(t, wire, 1)
			if records[0]["type"] != "abort" || len(tab.state.Steering) != 1 {
				t.Fatal("interrupt cleared queue or dispatched wrong command")
			}
			if (state == "streaming" || state == "tool") && !tab.state.Busy {
				t.Fatal("abort ack falsely settled agent")
			}
		})
	}
}
func TestEarlyEscapeRetriesOnLateAgentStartAndSettles(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	client, wire := fakeClient(t)
	tab.client = client
	tab.sending = true
	tab.awaitingStart = true
	first := commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEscape})).(replyMsg)
	m.reply(tab, first)
	_, cmd := m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_start"}`)}})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatal("late agent_start did not schedule retry")
	}
	reply := batch[1]().(replyMsg)
	if reply.op != "abort" {
		t.Fatal(reply.op)
	}
	wireRecords(t, wire, 2)
	if !tab.state.Busy {
		t.Fatal("late active agent was marked idle")
	}
	m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_settled"}`)}})
	if tab.state.Busy || tab.interruptRequested || tab.awaitingStart {
		t.Fatal("interrupt state leaked past settled")
	}
}
func TestEscapeScopeAndRemapping(t *testing.T) {
	m := testModel(t, `{"app.interrupt":"alt+x"}`)
	first := m.current()
	a, wireA := fakeClient(t)
	first.client = a
	first.state.Busy = true
	second := m.addTab("")
	b, _ := fakeClient(t)
	second.client = b
	second.state.Busy = true
	m.activate(0)
	if cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil {
		t.Fatal("disabled default Escape still aborted")
	}
	reply := commandResult(m.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt})).(replyMsg)
	if reply.id != first.id || reply.op != "abort" {
		t.Fatal(reply)
	}
	wireRecords(t, wireA, 1)
	if second.interruptRequested || !second.state.Busy {
		t.Fatal("other session interrupted")
	}
	first.dialogs = []*dialog{m.newDialog("input", "extension question", "")}
	first.interruptRequested = false
	m.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt})
	if first.interruptRequested {
		t.Fatal("modal cancellation leaked to global interrupt")
	}
}
