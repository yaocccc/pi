package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestForkDisplayAliasUsesNativeEntry(t *testing.T) {
	for _, tc := range []struct {
		name, selected string
		reconcile      bool
	}{
		{"persisted-earlier", "old", true},
		{"persisted-last", "new", true},
		{"live-last", "new", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, source, _, snapshot := uiForkFixture(t)
			old := source.state
			source.state = session.State{ID: old.ID, File: old.File, Name: old.Name, Cwd: old.Cwd}
			var tree struct {
				Entries []struct {
					ID      string
					Message json.RawMessage
				}
			}
			if err := json.Unmarshal(snapshot, &tree); err != nil {
				t.Fatal(err)
			}
			selected := ""
			for _, entry := range tree.Entries {
				event, _ := json.Marshal(map[string]any{"type": "message_end", "message": entry.Message})
				source.state.Apply(event)
				if entry.ID == tc.selected {
					for _, item := range source.state.Items {
						if item.Role == "user" {
							selected = strings.TrimSuffix(item.ID, "/0")
						}
					}
				}
			}
			if !strings.HasPrefix(selected, "live-") {
				t.Fatalf("expected live alias, got %q", selected)
			}
			if tc.reconcile {
				if err := source.state.LoadEntries(snapshot); err != nil {
					t.Fatal(err)
				}
			}
			// Reproduce the original error: a display ID is not a backend entry ID.
			if _, err := session.PreviewFork(selected, snapshot); err == nil {
				t.Fatal("fixture no longer reproduces alias mismatch")
			}
			cmd := m.requestFork(source, selected)
			if cmd == nil {
				t.Fatal("fork not requested")
			}
			response := cmd().(forkSnapshotMsg)
			response.entries = snapshot
			write := m.forkSnapshot(response)
			if write == nil {
				t.Fatal("alias fork rejected")
			}
			result := write().(forkCreatedMsg)
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.fork.Origin.Fork.UserEntryID != tc.selected {
				t.Fatalf("wrong native user: %+v", result.fork.Origin.Fork)
			}
		})
	}
}
