package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/codex"
)

type quotaMsg struct {
	key        string
	generation uint64
	snapshot   codex.Snapshot
}

func (m *Model) quotaIdentity() (string, codex.Identity) {
	t := m.current()
	if t == nil || t.state.Model.Provider != "openai-codex" {
		return "", codex.Identity{}
	}
	id := codex.Identity{Provider: t.state.Model.Provider, BaseURL: t.state.Model.BaseURL}
	return id.Provider + "\n" + id.BaseURL, id
}
func (m *Model) pollQuota(now time.Time) tea.Cmd {
	if t := m.current(); t == nil || t.state.Model.Provider == "" {
		return nil // Session initialization must not invalidate cached quota.
	}
	key, id := m.quotaIdentity()
	if key != m.quotaKey {
		m.quotaGeneration++
		m.quotaKey = key
		m.quotaChecked = time.Time{}
		m.quotaSnapshot = codex.Snapshot{}
		m.quota.Cancel()
	}
	if key == "" || m.quotaPending || (!m.quotaChecked.IsZero() && now.Sub(m.quotaChecked) < time.Minute) {
		return nil
	}
	m.quotaPending = true
	m.quotaChecked = now
	generation, client := m.quotaGeneration, m.quota
	return func() tea.Msg {
		snapshot, _ := client.Fetch(id) // quota errors are optional metadata, never chat/errors containing credentials.
		return quotaMsg{key: key, generation: generation, snapshot: snapshot}
	}
}
