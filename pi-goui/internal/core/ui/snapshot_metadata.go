package ui

import (
	"context"
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"
	rpc "pi-goui/internal/runtime"
)

// Optional metadata never delays committing the authoritative session body.
type snapshotMetadataMsg struct {
	id              int
	revision        uint64
	client          *rpc.Client
	file, sourceID  string
	stats, commands json.RawMessage
}

func (m *Model) snapshotMetadata(t *tab) tea.Cmd {
	if t.client == nil || t.dead {
		return nil
	}
	result := snapshotMetadataMsg{id: t.id, revision: t.revision, client: t.client, file: t.state.File, sourceID: t.state.ID}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if raw, err := result.client.Call(ctx, map[string]any{"type": "get_session_stats"}); err == nil {
			result.stats = raw
		}
		if raw, err := result.client.Call(ctx, map[string]any{"type": "get_commands"}); err == nil {
			result.commands = raw
		}
		return result
	}
}
func (m *Model) applySnapshotMetadata(v snapshotMetadataMsg) {
	t := m.byID(v.id)
	if t == nil || t.dead || t.client != v.client || t.revision != v.revision || t.state.File != v.file || t.state.ID != v.sourceID || t.ownRunActive() {
		return
	}
	if len(v.stats) > 0 {
		_ = t.state.LoadStats(v.stats)
	}
	if len(v.commands) > 0 {
		var list struct {
			Commands []command `json:"commands"`
		}
		if json.Unmarshal(v.commands, &list) == nil {
			t.commands = list.Commands
		}
	}
	// Stats and command completion do not change transcript layout. Invalidate
	// neither its cache nor an in-flight layout when optional metadata arrives.
}
