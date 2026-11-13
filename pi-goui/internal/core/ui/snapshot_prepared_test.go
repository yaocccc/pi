package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func BenchmarkPreparedBackgroundSnapshot(b *testing.B) {
	root := b.TempDir()
	cfg, err := config.Load(root)
	if err != nil {
		b.Fatal(err)
	}
	m := New(Options{Cwd: root, NoSession: true}, cfg)
	b.Cleanup(func() { _ = m.Close() })
	tab := m.addTab("")
	m.active = 0
	m.document(m.current())
	var entries []map[string]any
	parent := ""
	for i := 0; i < 320; i++ {
		id := fmt.Sprintf("a%d", i)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": map[string]any{"role": "assistant", "content": strings.Repeat("x", 32*1024), "stopReason": "stop"}})
		parent = id
	}
	raw, _ := json.Marshal(map[string]any{"entries": entries, "leafId": parent})
	prepared, err := tab.state.SnapshotPreparation().Prepare(nil, raw, nil)
	if err != nil {
		b.Fatal(err)
	}
	message := snapshotMsg{id: tab.id, revision: tab.revision, entries: raw, prepared: prepared, preparationDone: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Update(message)
	}
}

func TestUnpreparedHistoryNeverLoadsOnEventLoop(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.state.Name = "original"
	m.Update(snapshotMsg{id: tab.id, revision: tab.revision, state: json.RawMessage(`{"sessionName":"unexpected"}`), entries: json.RawMessage(`{`)})
	if tab.state.Name != "original" || tab.status != "history snapshot was not prepared" {
		t.Fatalf("unprepared history reached the loader: name=%q status=%q", tab.state.Name, tab.status)
	}
	count := len(m.tabs)
	m.forkCreated(forkCreatedMsg{id: tab.id, fork: session.Fork{Entries: json.RawMessage(`{`)}})
	if len(m.tabs) != count {
		t.Fatal("unprepared fork opened a partially initialized tab")
	}
}

func TestFailedPreparationStillSchedulesQueuedRefresh(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	client, _ := fakeClient(t)
	tab.client = client
	tab.refreshing, tab.refreshAgain = true, true
	m.Update(snapshotMsg{id: tab.id, revision: tab.revision, client: client, file: tab.state.File, sourceID: tab.state.ID, preparationDone: true, prepareErr: errors.New("invalid history")})
	if !tab.refreshing || tab.refreshAgain {
		t.Fatal("failed preparation lost the already queued refresh")
	}
	// The next command is intentionally not executed: only scheduling is tested.
}

func TestRefreshPreparesBeforeReturningToUI(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	client, _ := fakeClient(t)
	tab.client = client
	message := m.refresh(tab)().(snapshotMsg)
	if message.err != nil || message.prepareErr != nil || !message.preparationDone || message.prepared == nil {
		t.Fatalf("refresh returned unprepared history: %+v", message)
	}
	// The consumer must use the immutable candidate, not decode RPC bytes a
	// second time on the UI goroutine.
	message.entries = json.RawMessage(`invalid JSON no longer needed by commit`)
	status := tab.status
	m.Update(message)
	if tab.state.ID != "fake" || tab.state.Model.ID != "one" || tab.status != status {
		t.Fatal("prepared refresh did not commit")
	}
}

func TestPreparedRefreshRejectsChangedOwner(t *testing.T) {
	for _, change := range []string{"backend", "path", "identity", "revision", "closed"} {
		t.Run(change, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			client, _ := fakeClient(t)
			tab.client = client
			tab.state.ID = "original"
			tab.state.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"keep"}}`))
			message := m.refresh(tab)().(snapshotMsg)
			before := append([]session.Item(nil), tab.state.Items...)
			switch change {
			case "backend":
				tab.client, _ = fakeClient(t)
			case "path":
				tab.state.File = "another.jsonl"
			case "identity":
				tab.state.ID = "another"
			case "revision":
				tab.revision++
			case "closed":
				m.tabs = nil
			}
			m.Update(message)
			if !reflect.DeepEqual(tab.state.Items, before) || tab.state.Model.ID == "one" {
				t.Fatal("stale prepared history replaced the owner's current state")
			}
		})
	}
}

func TestPreparedRefreshFailureDoesNotPublishPartialState(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	if err := tab.state.LoadState(json.RawMessage(`{"sessionId":"original","sessionFile":"old.jsonl"}`)); err != nil {
		t.Fatal(err)
	}
	tab.state.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"keep"}}`))
	before := append([]session.Item(nil), tab.state.Items...)
	state := json.RawMessage(`{"sessionId":"new","sessionFile":"new.jsonl"}`)
	entries := json.RawMessage(`{"entries":[{"id":"x","parentId":"missing"}],"leafId":"x"}`)
	prepared, err := tab.state.SnapshotPreparation().Prepare(state, entries, nil)
	if err == nil || prepared != nil {
		t.Fatal("bad history produced a prepared candidate")
	}
	m.Update(snapshotMsg{id: tab.id, revision: tab.revision, state: state, entries: entries, prepared: prepared, preparationDone: true, prepareErr: err, origin: &session.Header{ID: "new"}})
	if tab.state.ID != "original" || tab.state.File != "old.jsonl" || tab.origin.ID == "new" || !reflect.DeepEqual(tab.state.Items, before) {
		t.Fatal("failed history published part of a replacement session")
	}
}

func TestPreparedRefreshKeepsBackgroundOwner(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	owner.client, _ = fakeClient(t)
	message := m.refresh(owner)().(snapshotMsg)
	other := m.addTab("")
	m.Update(message)
	if m.current() != other || owner.state.ID != "fake" || other.state.ID != "" {
		t.Fatal("background preparation applied to the active tab instead of owner")
	}
}
