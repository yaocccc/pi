package ui

import (
	"sort"
	"time"

	"pi-goui/internal/render"
)

func (t *tab) dismissCompletedWorking() {
	if !t.working.active {
		t.working = workingState{}
	}
}

// Only this method feeds styled, trusted foreground sequences to the status row.
func (m *Model) workingLine(t *tab) string {
	line := t.working.line(time.Now(), m.cfg.Theme)
	if line == "" || t.dead || t.state.Compacting || t.sending || len(t.dialogs) > 0 {
		status := m.status(t)
		if status == "Ready" || status == "ready" {
			status = ""
		}
		line = ""
		if status != "" {
			line = m.paint(render.Clean(status), m.cfg.Theme.Muted)
		}
	} else if t.status != "" && t.status != "Ready" {
		line += " · " + m.paint(render.Clean(t.status), m.cfg.Theme.Muted)
	}
	keys := make([]string, 0, len(t.statuses))
	for key := range t.statuses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		line += "  " + m.paint(render.Clean(t.statuses[key]), m.cfg.Theme.Muted)
	}
	return line
}
