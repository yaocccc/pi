package ui

import (
	"encoding/json"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	rpc "pi-goui/internal/runtime"
)

type eventBatchMsg struct {
	id     int
	client *rpc.Client
	events []rpc.Event
	closed bool
	err    error
}

func nextEventCommand(id int, client *rpc.Client) tea.Cmd {
	return func() tea.Msg {
		events, ok := <-client.EventBatches()
		batch := eventBatchMsg{id: id, client: client, events: events, closed: !ok}
		if batch.closed {
			batch.err = client.Err()
		}
		return batch
	}
}

func streamUpdate(event rpc.Event) bool {
	if event.Err != nil {
		return false
	}
	var envelope struct{ Type string }
	if json.Unmarshal(event.Data, &envelope) != nil {
		return false
	}
	return envelope.Type == "message_update" || envelope.Type == "tool_execution_update"
}

func applyWorkingEvent(t *tab, raw json.RawMessage) {
	if t.client == nil || !t.client.Shared() || !t.working.applyShared(raw) {
		t.working.apply(raw, time.Now())
	}
}

// Only adjacent stream updates share a projection. Control events, unknown
// events, snapshots, errors and extension requests retain their original order
// and see the fully projected state preceding them.
func (m *Model) applyEventBatch(t *tab, events []rpc.Event) (cmds []tea.Cmd, renderNow bool) {
	for i := 0; i < len(events); {
		if cmd := m.sharedHistorySnapshot(t, events[i]); cmd != nil {
			t.snapshotTail = append([]rpc.Event(nil), events[i+1:]...)
			return append(cmds, cmd), true
		}
		if streamUpdate(events[i]) {
			end := i + 1
			for end < len(events) && streamUpdate(events[end]) {
				end++
			}
			raw := make([]json.RawMessage, 0, end-i)
			for _, event := range events[i:end] {
				applyWorkingEvent(t, event.Data)
				raw = append(raw, event.Data)
				t.revision++
			}
			if t.state.ApplyBatch(raw) {
				cmds = append(cmds, m.refresh(t))
			}
			t.invalidateStream()
			i = end
		} else {
			next, render := m.applySingleEvent(t, events[i])
			cmds = append(cmds, next...)
			renderNow = renderNow || render
			i++
		}
		// Preserve control-boundary extension effects before applying the next
		// event. State.ApplyBatch still observes every individual raw update.
		cmds = append(cmds, t.syncExtensions())
	}
	return cmds, renderNow
}

func (m *Model) applySingleEvent(t *tab, event rpc.Event) (cmds []tea.Cmd, renderNow bool) {
	if event.Err != nil {
		t.lastEventErr = event.Err
		t.status = event.Err.Error()
	}
	if len(event.Data) == 0 {
		return cmds, false
	}
	if handled, cmd := m.sharedSessionEvent(t, event.Data); handled {
		return append(cmds, cmd), true
	}
	var env struct{ Type string }
	_ = json.Unmarshal(event.Data, &env)
	wasWorking := t.working.Active
	if env.Type == "agent_start" && !wasWorking {
		t.workingStart = len(t.state.Items)
	}
	applyWorkingEvent(t, event.Data)
	if env.Type == "goui_working_snapshot" {
		return cmds, false
	}
	if env.Type == "extension_ui_request" {
		return append(cmds, m.extension(t, event.Data)), false
	}
	t.revision++
	if t.state.Apply(event.Data) {
		cmds = append(cmds, m.refresh(t))
	}
	t.invalidateStream()
	if env.Type == "agent_start" {
		t.status = ""
		t.sending = false
		t.awaitingStart = false
		if t.interruptRequested {
			t.status = "Stopping…"
			cmds = append(cmds, call(t, "abort", nil))
		}
	}
	if env.Type == "agent_settled" {
		if wasWorking {
			t.saveAssistantStats()
		}
		t.status = "Ready"
		t.sending = false
		t.awaitingStart, t.interruptRequested = false, false
	}
	if t != m.current() && (env.Type == "message_end" || env.Type == "agent_settled") {
		t.unread++
	}
	return cmds, false
}

func (m *Model) closeEventStream(t *tab, err error) {
	if err != nil {
		t.lastEventErr = err
	}
	if t.mutation != nil {
		t.mutation.disconnected = true
	}
	t.revision++
	t.dead = true
	t.state.Busy = false
	t.sending = false
	if t.resume.pending {
		err := t.lastEventErr
		if err == nil {
			err = errors.New("连接在初始化完成前关闭")
		}
		m.failResume(t, err)
	} else {
		t.status = "pi stopped"
		if t.lastEventErr != nil {
			t.status += " · " + t.lastEventErr.Error()
		}
		t.status += " · /new to start another session"
	}
}
