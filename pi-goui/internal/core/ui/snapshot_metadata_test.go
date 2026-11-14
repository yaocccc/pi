package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestInitialSharedSnapshotDoesNotDuplicateLoad(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "during-load", true: "after-load"}[late], func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			tab.client, _ = fakeClient(t)
			load := m.refresh(tab)
			if late {
				m.Update(load())
			}
			revision, again, refreshing := tab.revision, tab.refreshAgain, tab.refreshing
			handled, cmd := m.sharedSessionEvent(tab, json.RawMessage(`{"type":"goui_session_snapshot","initial":true}`))
			if !handled || cmd != nil || tab.revision != revision || tab.refreshAgain != again || tab.refreshing != refreshing {
				t.Fatal("initial handshake duplicated or invalidated authoritative load")
			}
			if !late {
				m.Update(load())
			}
			if tab.refreshAgain {
				t.Fatal("queued a redundant history read")
			}
		})
	}
}

func TestHistoryLoadDoesNotQueryOptionalMetadata(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	var wire string
	tab.client, wire = fakeClient(t)
	snapshot := m.refresh(tab)().(snapshotMsg)
	if snapshot.err != nil || snapshot.prepareErr != nil || snapshot.prepared == nil {
		t.Fatalf("history failed: %v %v", snapshot.err, snapshot.prepareErr)
	}
	requests := wireRecords(t, wire, 2)
	if len(requests) != 2 || requests[0]["type"] != "get_state" || requests[1]["type"] != "get_entries" {
		t.Fatalf("body load waited on optional requests: %#v", requests)
	}
	m.Update(snapshot)
	if tab.state.ID != "fake" || tab.refreshing {
		t.Fatal("body not committed before metadata")
	}
	metadata := m.snapshotMetadata(tab)().(snapshotMetadataMsg)
	requests = wireRecords(t, wire, 4)
	if len(requests) != 4 || requests[2]["type"] != "get_session_stats" || requests[3]["type"] != "get_commands" {
		t.Fatalf("optional metadata was not separately requested: %#v", requests)
	}
	m.Update(metadata)
}

func TestSnapshotMetadataPreservesTranscriptLayout(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "layout-pending"}[pending], func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			tab.state.ID, tab.state.File = "saved", "saved.jsonl"
			tab.state.Items = []session.Item{{ID: "body", Role: "assistant", Text: "published body"}}
			m.document(tab)
			tab.editor.SetValue("local draft")
			before := tab.doc.Text
			if pending {
				tab.state.Items[0].Text = strings.Repeat("**Prepared history**\n\n", 512)
				tab.invalidateStream()
				if m.scheduleDocument(tab) == nil {
					t.Fatal("missing background layout")
				}
			}
			generation, job := tab.layoutGeneration, tab.documentJob
			m.Update(snapshotMetadataMsg{id: tab.id, revision: tab.revision, client: tab.client,
				file: tab.state.File, sourceID: tab.state.ID,
				stats:    json.RawMessage(`{"tokens":{"input":19,"output":7}}`),
				commands: json.RawMessage(`{"commands":[{"name":"new-command"}]}`)})
			if tab.layoutGeneration != generation || tab.documentJob != job || tab.doc.Text != before {
				t.Fatal("optional metadata invalidated or synchronously rebuilt the transcript")
			}
			if tab.state.Stats.Input != 19 || len(tab.commands) != 1 || tab.commands[0].Name != "new-command" || tab.editor.Value() != "local draft" {
				t.Fatal("metadata or local draft was lost")
			}
			if pending {
				m.Update(job.render())
				if tab.documentJob != nil || tab.dirty || !strings.Contains(tab.doc.Text, "Prepared history") {
					t.Fatal("metadata made the prepared layout stale")
				}
			}
		})
	}
}

func TestSnapshotMetadataOwnership(t *testing.T) {
	for _, kind := range []string{"current", "revision", "file", "session", "client", "dead", "busy", "closed"} {
		t.Run(kind, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			tab.client, _ = fakeClient(t)
			tab.state.ID = "owner"
			tab.state.File = "owner.jsonl"
			v := snapshotMetadataMsg{id: tab.id, revision: tab.revision, client: tab.client, file: tab.state.File, sourceID: tab.state.ID, stats: json.RawMessage(`{"tokens":{"input":19,"output":7}}`), commands: json.RawMessage(`{"commands":[{"name":"late-command"}]}`)}
			switch kind {
			case "revision":
				tab.revision++
			case "file":
				tab.state.File = "other"
			case "session":
				tab.state.ID = "other"
			case "client":
				tab.client = nil
			case "dead":
				tab.dead = true
			case "busy":
				tab.state.Busy = true
			case "closed":
				m.tabs = nil
			}
			m.applySnapshotMetadata(v)
			if kind == "current" {
				if tab.state.Stats.Input != 19 || len(tab.commands) != 1 {
					t.Fatal("owned metadata not applied")
				}
			} else if tab.state.Stats.Input != 0 || len(tab.commands) != 0 {
				t.Fatal("late metadata crossed owner/revision")
			}
		})
	}
}
