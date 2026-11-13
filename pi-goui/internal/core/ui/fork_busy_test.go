package ui

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/core/rpc"
	"pi-goui/internal/core/session"
)

func TestForkCompletedHistoryWhileRunning(t *testing.T) {
	for _, active := range []string{"busy", "compacting", "sending", "awaiting-start", "working"} {
		t.Run(active, func(t *testing.T) {
			m, source, wire, snapshot := uiForkFixture(t)
			original, _ := os.ReadFile(source.state.File)
			switch active {
			case "busy":
				source.state.Busy = true
			case "compacting":
				source.state.Compacting = true
			case "sending":
				source.sending = true
			case "awaiting-start":
				source.awaitingStart = true
			case "working":
				source.working.active = true
			}
			if cmd := m.requestFork(source, "new"); cmd != nil || source.forkPending {
				t.Fatal("active latest round was accepted")
			}
			cmd := m.requestFork(source, "old")
			if cmd == nil || !source.forkPending {
				t.Fatal("completed historical round was blocked")
			}
			if m.requestFork(source, "old") != nil {
				t.Fatal("duplicate snapshot accepted")
			}
			v := cmd().(forkSnapshotMsg)
			v.entries = snapshot
			m.Update(eventMsg{id: source.id, event: rpc.Event{Data: json.RawMessage(`{"type":"message_start","message":{"role":"assistant","timestamp":200,"content":[]}}`)}})
			m.Update(eventMsg{id: source.id, event: rpc.Event{Data: json.RawMessage(`{"type":"message_update","message":{"role":"assistant","timestamp":200,"content":[{"type":"text","text":"still generating later turn"}]}}`)}})
			if source.revision == v.revision {
				t.Fatal("test did not exercise streaming revision changes")
			}
			beforeItems := append([]session.Item(nil), source.state.Items...)
			client := source.client
			write := m.forkSnapshot(v)
			if write == nil || len(source.dialogs) != 0 {
				t.Fatal("later deltas blocked direct historical fork")
			}
			if m.requestFork(source, "old") != nil {
				t.Fatal("duplicate write accepted")
			}
			result := write().(forkCreatedMsg)
			if result.err != nil {
				t.Fatal(result.err)
			}
			m.forkCreated(result) // Do not execute the new tab's real RPC startup.
			if !source.ownRunActive() || source.client != client || source.editor.Value() != "original draft" || !reflect.DeepEqual(beforeItems, source.state.Items) {
				t.Fatal("historical fork changed the active source")
			}
			after, _ := os.ReadFile(source.state.File)
			if string(original) != string(after) {
				t.Fatal("source history was changed")
			}
			fork := m.current()
			if len(m.tabs) != 2 || fork == source || fork.state.Name != "[1]Original" || source.forkPending {
				t.Fatal("fork did not finish independently")
			}
			data, err := os.ReadFile(fork.state.File)
			if err != nil || !strings.Contains(string(data), "earlier answer") || strings.Contains(string(data), `"id":"new"`) || strings.Contains(string(data), "still generating") {
				t.Fatalf("incorrect copied round: %s, %v", data, err)
			}
			for _, request := range wireRecords(t, wire, 1) {
				if request["type"] != "get_entries" {
					t.Fatalf("source received mutation: %v", request)
				}
			}
		})
	}
}

func TestForkHistoricalSnapshotRejectsChangedCopiedHistory(t *testing.T) {
	for _, scenario := range []string{"user", "reply", "boundary", "branch", "request-history", "client-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			m, source, _, snapshot := uiForkFixture(t)
			changed := snapshot
			switch scenario {
			case "user", "request-history":
				changed = json.RawMessage(strings.Replace(string(snapshot), `"content":"first"`, `"content":"changed"`, 1))
			case "reply":
				changed = json.RawMessage(strings.Replace(string(snapshot), "earlier answer", "changed reply", 1))
			case "boundary":
				changed = json.RawMessage(strings.Replace(string(snapshot), `"content":"last"`, `"content":"changed next"`, 1))
			case "branch":
				changed = json.RawMessage(strings.ReplaceAll(string(snapshot), `"new"`, `"sibling"`))
			}
			if scenario == "request-history" {
				if err := source.state.LoadEntries(changed); err != nil {
					t.Fatal(err)
				}
			}
			source.state.Busy = true
			cmd := m.requestFork(source, "old")
			if cmd == nil {
				t.Fatal("historical request rejected before snapshot")
			}
			v := cmd().(forkSnapshotMsg)
			v.entries = snapshot
			if scenario == "request-history" {
				if err := source.state.LoadEntries(snapshot); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "client-replaced" {
				source.client, _ = fakeClient(t)
			} else if err := source.state.LoadEntries(changed); err != nil {
				t.Fatal(err)
			}
			source.revision++
			verify := m.forkSnapshot(v)
			if verify != nil {
				next, ok := verify().(forkSnapshotMsg)
				if !ok {
					t.Fatal("changed prefix scheduled persistence instead of revalidation")
				}
				verify = m.forkSnapshot(next)
			}
			if verify != nil || source.forkPending {
				t.Fatal("changed history/identity accepted")
			}
			assertForkNotWritten(t, m, source)
		})
	}
}

func TestForkHistoricalReplyMustBeComplete(t *testing.T) {
	for _, reason := range []string{"toolUse", "error", "aborted", ""} {
		t.Run(reason, func(t *testing.T) {
			m, source, _, snapshot := uiForkFixture(t)
			snapshot = json.RawMessage(strings.Replace(string(snapshot), `"stopReason":"stop"`, `"stopReason":"`+reason+`"`, 1))
			if err := source.state.LoadEntries(snapshot); err != nil {
				t.Fatal(err)
			}
			source.state.Busy = true
			setForkFixtureSnapshot(t, snapshot)
			v := m.requestFork(source, "old")().(forkSnapshotMsg)
			if m.forkSnapshot(v) != nil || source.forkPending || !strings.Contains(source.toast.text, "尚未完成") {
				t.Fatal("unfinished historical reply was accepted")
			}
			assertForkNotWritten(t, m, source)
		})
	}
}

func TestForkHistoricalSnapshotAllowsLaterPersistedDescendant(t *testing.T) {
	m, source, _, snapshot := uiForkFixture(t)
	source.state.Busy = true
	v := m.requestFork(source, "old")().(forkSnapshotMsg)
	v.entries = snapshot
	var data struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(snapshot, &data); err != nil {
		t.Fatal(err)
	}
	data.Entries = append(data.Entries, json.RawMessage(`{"type":"message","id":"later-user","parentId":"error","message":{"role":"user","content":"another later round"}}`))
	data.LeafID = "later-user"
	later, _ := json.Marshal(data)
	if err := source.state.LoadEntries(later); err != nil {
		t.Fatal(err)
	}
	source.revision++
	if m.forkSnapshot(v) == nil {
		t.Fatal("persisting a later descendant invalidated the completed prefix")
	}
}
