package ui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

type snapshotMsg struct {
	id             int
	revision       uint64
	origin         *session.Header
	err            error
	client         *rpc.Client
	file, sourceID string
	prepared       *session.PreparedSnapshot
	prepareErr     error
}

func (m *Model) refresh(t *tab) tea.Cmd {
	if t.client == nil || t.dead || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.refreshing {
		t.refreshAgain = true
		return nil
	}
	t.refreshing = true
	t.refreshAgain = false
	id, c, file, revision := t.id, t.client, t.state.File, t.revision
	sourceID := t.state.ID
	preparation := t.state.SnapshotPreparation()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s := snapshotMsg{id: id, revision: revision, client: c, file: file, sourceID: sourceID}
		var state, entries json.RawMessage
		state, s.err = c.Call(ctx, map[string]any{"type": "get_state"})
		if s.err != nil {
			return s
		}
		var current struct{ SessionFile, SessionID string }
		if json.Unmarshal(state, &current) == nil && current.SessionFile != "" {
			file = current.SessionFile
		}
		if file != "" {
			h, headerErr := session.ReadHeader(file, current.SessionID)
			s.origin = &h
			// Some endpoints omit a known saved path. Preserve it only after
			// confirming that its native header still identifies this session.
			if headerErr == nil && current.SessionFile == "" && current.SessionID != "" {
				var fields map[string]json.RawMessage
				if json.Unmarshal(state, &fields) == nil {
					fields["sessionFile"], _ = json.Marshal(file)
					state, _ = json.Marshal(fields)
				}
			}
		}
		entries, s.err = c.Call(ctx, map[string]any{"type": "get_entries"})
		if s.err != nil && file != "" {
			entries, s.err = session.ReadFile(file)
		}
		if s.err != nil {
			// State metadata and history are one commit. A failed history
			// fetch must not publish only a replacement session identity.
			return s
		}
		// Commit the authoritative body before optional statistics/commands.
		s.prepared, s.prepareErr = preparation.Prepare(state, entries, nil)
		return s
	}
}

func (m *Model) applySessionSnapshot(t *tab, v snapshotMsg) []tea.Cmd {
	var cmds []tea.Cmd
	t.refreshing = false
	current := !t.dead && v.revision == t.revision
	// A transport error belongs to the connection; preparation errors belong
	// only to the revision whose history was being prepared.
	err := v.err
	if err != nil {
		t.status = "Refresh: " + err.Error()
	} else if current && v.prepared == nil {
		err = v.prepareErr
		if err == nil {
			err = errors.New("history snapshot was not prepared")
		}
		t.status = err.Error()
	}
	if err != nil && t.resume.pending {
		m.failResume(t, err)
		cmds = append(cmds, m.closeCycle(t))
	}
	if current && v.prepared != nil {
		t.state.ApplySnapshot(v.prepared)
		t.confirmPromptEcho()
		if t.resume.pending && v.prepared.HasState() && v.prepared.HasEntries() {
			t.resume.authoritative = true
			if v.err == nil {
				t.status = "Ready"
			}
			cmds = append(cmds, m.primeCycle(t))
		}
		if v.origin != nil {
			t.origin = *v.origin
		}
		t.invalidateDocument()
		if v.prepared.HasEntries() {
			m.finishOriginJump(t)
		}
		if len(t.history) == 0 {
			for _, it := range t.state.Items {
				if it.Role == "user" && strings.TrimSpace(it.Text) != "" {
					t.history = append(t.history, it.Text)
				}
			}
		}
		cmds = append(cmds, m.snapshotMetadata(t))
	}
	if !t.dead && v.revision != t.revision {
		t.refreshAgain = true
	}
	// During a run, keep consuming live events rather than spin on snapshots
	// that immediately become stale. agent_settled requests the final refresh.
	// An ordered initialization can retry a local revision change: live
	// deltas cannot starve preparation beyond its next FIFO fence.
	if t.refreshAgain && !t.ownRunActive() {
		cmds = append(cmds, m.refresh(t))
	}
	return cmds
}
