package ui

import (
	"errors"
	"strings"
	"testing"

	rpc "pi-goui/internal/runtime"
)

func TestStoppedPreservesEventError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "error", err: errors.New("read pi stdout: connection reset"), want: "pi stopped · history and draft retained · read pi stdout: connection reset"},
		{name: "no error", want: "pi stopped · history and draft retained"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t, "{}")
			tab := m.current()
			tab.client = &rpc.Client{}
			tab.status = "unrelated status must not become a stop reason"
			if tc.err != nil {
				m.Update(eventMsg{id: tab.id, client: tab.client, event: rpc.Event{Err: tc.err}})
				if tab.status != tc.err.Error() {
					t.Fatalf("event error not shown: %q", tab.status)
				}
				// Other UI activity must not erase the recorded transport error.
				tab.status = "another status"
			}
			tab.state.Busy, tab.sending = true, true
			_, cmd := m.Update(eventMsg{id: tab.id, client: tab.client, closed: true})
			if tab.status != tc.want {
				t.Fatalf("status = %q, want %q", tab.status, tc.want)
			}
			if !tab.dead || tab.state.Busy || tab.sending || cmd == nil {
				t.Fatal("closed event did not preserve stop/cleanup semantics")
			}
		})
	}
}

func TestStoppedErrorBelongsToOriginalClient(t *testing.T) {
	m := testModel(t, "{}")
	tab := m.current()
	old := &rpc.Client{}
	tab.client = old
	m.Update(eventMsg{id: tab.id, client: old, event: rpc.Event{Err: errors.New("old connection error")}})

	current := &rpc.Client{}
	// Do not run the returned commands: these clients are identity-only stubs.
	tab.cycle.phase = cycleStarting
	tab.cycle.generation++
	m.Update(openedMsg{id: tab.id, generation: tab.cycle.generation, client: current})
	status := tab.status
	for _, msg := range []eventMsg{
		{id: tab.id, client: old, event: rpc.Event{Err: errors.New("late old error")}},
		{id: tab.id, client: old, event: rpc.Event{Err: errors.New("late terminal failure")}, closed: true},
	} {
		_, cmd := m.Update(msg)
		if tab.dead || tab.client != current || tab.status != status || tab.lastEventErr != nil || cmd != nil {
			t.Fatal("replaced client's event affected the current connection")
		}
	}
	m.Update(eventMsg{id: tab.id, client: current, closed: true})
	if tab.status != "pi stopped · history and draft retained" || !tab.dead {
		t.Fatalf("new connection inherited old error: %q", tab.status)
	}
}

func TestStoppedWhileResumingPreservesFailureSemantics(t *testing.T) {
	for _, reason := range []string{"", "shared transport disconnected"} {
		t.Run(reason, func(t *testing.T) {
			m := testModel(t, "{}")
			tab := m.current()
			tab.client = &rpc.Client{}
			tab.resume.pending = true
			tab.editor.SetValue("keep draft")
			if reason != "" {
				m.Update(eventMsg{id: tab.id, client: tab.client, event: rpc.Event{Err: errors.New(reason)}})
			} else {
				reason = "connection closed during initialization"
			}
			m.Update(eventMsg{id: tab.id, client: tab.client, closed: true})
			want := "连接会话失败：" + reason + "；历史和未发送内容已保留"
			if tab.status != want || !tab.dead || tab.resume.pending || !tab.resume.failed || tab.editor.Value() != "keep draft" {
				t.Fatalf("resume failure state not retained: status=%q resume=%+v", tab.status, tab.resume)
			}
		})
	}
}

func TestStoppedUsesTerminalFailure(t *testing.T) {
	for _, resuming := range []bool{false, true} {
		m := testModel(t, "{}")
		tab := m.current()
		tab.client = &rpc.Client{}
		tab.resume.pending = resuming
		tab.lastEventErr = errors.New("earlier nonfatal warning")
		tab.editor.SetValue("keep draft")
		reason := "shared event backlog exceeded; reconnect and rebuild session state"
		m.Update(eventMsg{id: tab.id, client: tab.client, event: rpc.Event{Err: errors.New(reason)}, closed: true})
		if !tab.dead || !strings.Contains(tab.status, reason) || strings.Contains(tab.status, "earlier nonfatal warning") {
			t.Fatalf("terminal error not shown (resuming=%v): %q", resuming, tab.status)
		}
		if resuming && (tab.resume.pending || !tab.resume.failed || tab.editor.Value() != "keep draft") {
			t.Fatal("terminal error changed resume failure semantics")
		}
	}
}

func TestNextEventCarriesTerminalFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t, "{}")
	tab := m.current()
	client, _ := fakeClient(t)
	tab.client = client
	cmd := nextEvent(tab)
	// Explicit close delivers no error event. The closed message must retrieve
	// the terminal failure separately, just as for an overflowed event queue.
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	msg := cmd().(eventBatchMsg)
	if !msg.closed || msg.client != client || msg.id != tab.id || msg.err == nil {
		t.Fatalf("closed message lost origin or error: %+v", msg)
	}
	if err := client.Err(); err == nil || msg.err.Error() != err.Error() {
		t.Fatalf("closed message error = %v, client error = %v", msg.err, err)
	}
	m.Update(msg)
	if !tab.dead || !strings.Contains(tab.status, msg.err.Error()) {
		t.Fatalf("terminal error not shown: %q", tab.status)
	}
}

func TestOpenedFailureDoesNotRetainPreviousEventError(t *testing.T) {
	m := testModel(t, "{}")
	tab := m.current()
	tab.client = &rpc.Client{}
	m.Update(eventMsg{id: tab.id, client: tab.client, event: rpc.Event{Err: errors.New("old transport failure")}})
	tab.cycle.phase = cycleStarting
	tab.cycle.generation++
	m.Update(openedMsg{id: tab.id, generation: tab.cycle.generation, err: errors.New("executable not found")})
	if tab.dead || tab.lastEventErr != nil || !strings.Contains(tab.status, "executable not found") || strings.Contains(tab.status, "old transport failure") {
		t.Fatalf("startup failure changed or inherited previous error: %q", tab.status)
	}
}
