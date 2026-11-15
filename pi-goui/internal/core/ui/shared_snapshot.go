package ui

import (
	"encoding/json"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

// This event is published at get_entries' backend wire position. All preceding
// deltas have been applied; later deltas stay in the bounded transport FIFO until
// preparation commits. State was queried earlier: its event-derived fields
// are merged against the pre-query metadata checkpoint, not accepted wholesale
// merely because this fence has the current revision. Revision and owner checks
// still guard the async result.
func (m *Model) sharedHistorySnapshot(t *tab, event rpc.Event) tea.Cmd {
	var envelope struct {
		Type           string
		Request        uint64
		State, Entries json.RawMessage
	}
	if json.Unmarshal(event.Data, &envelope) != nil || envelope.Type != "goui_history_snapshot" {
		return nil
	}
	owner := t.snapshotOwner
	if owner == nil || envelope.Request != owner.request || t.client != owner.client || t.state.File != owner.file || t.state.ID != owner.sourceID {
		return nil
	}
	s := *owner
	s.revision = t.revision
	s.state, s.entries = envelope.State, envelope.Entries
	preparation := t.state.SnapshotPreparation()
	t.snapshotPaused = true
	return func() tea.Msg {
		var current struct{ SessionFile, SessionID string }
		if json.Unmarshal(s.state, &current) == nil && current.SessionFile != "" {
			h, _ := session.ReadHeader(current.SessionFile, current.SessionID)
			s.origin = &h
		}
		s.prepared, s.prepareErr = preparation.PrepareAfter(s.state, s.entries, nil, s.metadataRevision)
		s.preparationDone = true
		return s
	}
}
