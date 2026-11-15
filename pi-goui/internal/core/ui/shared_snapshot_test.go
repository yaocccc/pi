package ui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	rpc "pi-goui/internal/runtime"
)

func orderedSnapshotFixture(t *testing.T) (*Model, *tab, rpc.Event) {
	t.Helper()
	m := testModel(t, "")
	owner := m.current()
	owner.client, _ = fakeClient(t)
	owner.resume.pending = true
	owner.refreshing = true
	owner.working.Active = true // Late attachment has never seen agent_start.
	owner.snapshotOwner = &snapshotMsg{id: owner.id, client: owner.client, request: 1}
	return m, owner, rpc.Event{Data: json.RawMessage(`{"type":"goui_history_snapshot","request":1,"state":{"sessionId":"late","isStreaming":true},"entries":{"entries":[{"type":"message","id":"u","message":{"role":"user","content":"history","timestamp":1}}],"leafId":"u"}}`)}
}

func TestSharedSnapshotFencePreparesWhileDeltasWait(t *testing.T) {
	m, owner, fence := orderedSnapshotFixture(t)
	before := []rpc.Event{
		{Data: json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"history","timestamp":1}}`)},
		{Data: json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[{"type":"text","text":"prefix"}],"timestamp":2}}`)},
	}
	batch := append(before, fence)
	for i := 0; i < 5; i++ {
		batch = append(batch, rpc.Event{Data: json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"+"}}`)})
	}
	cmds, _ := m.applyEventBatch(owner, batch)
	if !owner.snapshotPaused || len(owner.snapshotTail) != 5 || owner.revision != 2 {
		t.Fatalf("fence did not pause precisely: paused=%v tail=%d revision=%d", owner.snapshotPaused, len(owner.snapshotTail), owner.revision)
	}
	prepared := cmds[len(cmds)-1]().(snapshotMsg)
	if prepared.prepareErr != nil {
		t.Fatal(prepared.prepareErr)
	}
	_, continuation := m.Update(prepared)
	if owner.resume.pending || !owner.resume.authoritative || owner.refreshing || owner.snapshotPaused {
		t.Fatalf("initialization starved: %+v", owner.resume)
	}
	// The continuation replays the retained suffix before requesting another
	// batch. Preparation remains asynchronous and never applies those deltas.
	commands := continuation().(tea.BatchMsg)
	tail := commands[0]().(eventBatchMsg)
	m.Update(tail)
	var text string
	for _, item := range owner.state.Items {
		text += item.Text + "\n"
	}
	if strings.Count(text, "history") != 1 || strings.Count(text, "prefix+++++") != 1 {
		t.Fatalf("history/live content lost, duplicated or rewound: %q", text)
	}
	if !owner.ownRunActive() {
		t.Fatal("snapshot lost active run")
	}
	if owner.refreshing {
		t.Fatal("snapshot refresh busy loop")
	}
}

func TestSharedSnapshotFailureDoesNotRetryEveryDelta(t *testing.T) {
	m, owner, _ := orderedSnapshotFixture(t)
	failed := *owner.snapshotOwner
	failed.err = errors.New("get_entries unavailable")
	owner.revision++
	m.Update(failed)
	if owner.resume.pending || !owner.resume.failed || owner.refreshing {
		t.Fatal("failed ordered request kept initialization spinning")
	}
}

func TestSharedSnapshotKeepsBackgroundTabOwner(t *testing.T) {
	m, owner, fence := orderedSnapshotFixture(t)
	prepared := m.sharedHistorySnapshot(owner, fence)().(snapshotMsg)
	other := m.addTab("")
	m.Update(prepared)
	if !owner.resume.authoritative || other.state.ID != "" || m.current() != other {
		t.Fatal("ordered snapshot changed tabs or applied to the wrong owner")
	}
}

func TestOrderedSnapshotKeepsRevisionAndOwnerGuards(t *testing.T) {
	for _, change := range []string{"revision", "client", "session", "removed", "request"} {
		t.Run(change, func(t *testing.T) {
			m, owner, fence := orderedSnapshotFixture(t)
			cmd := m.sharedHistorySnapshot(owner, fence)
			result := cmd().(snapshotMsg)
			switch change {
			case "revision":
				owner.revision++
			case "client":
				owner.client, _ = fakeClient(t)
			case "session":
				owner.state.ID = "different"
			case "removed":
				m.tabs = nil
			case "request":
				owner.snapshotOwner.request++
			}
			m.Update(result)
			if owner.resume.authoritative || len(owner.state.Items) != 0 {
				t.Fatal("stale ordered snapshot was committed")
			}
		})
	}
}
