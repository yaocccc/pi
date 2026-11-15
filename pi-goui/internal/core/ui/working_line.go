package ui

import (
	"pi-goui/internal/core/config"
	"sort"
	"strings"
	"time"

	"pi-goui/internal/core/render"
)

// Freeze the same run metrics shown by working, attached to a visible block.
// Separate storage keeps them after the working row is dismissed or restarted.
func (t *tab) saveAssistantStats() {
	if t.working.Active || !t.working.Present {
		return
	}
	for i := len(t.state.Items) - 1; i >= t.workingStart; i-- {
		item := t.state.Items[i]
		if (item.Role == "assistant" && strings.TrimSpace(item.Text) != "") || item.Role == "tool" || item.Role == "toolResult" {
			if t.assistantStats == nil {
				t.assistantStats = map[string]string{}
			}
			t.assistantStats[item.ID] = strings.TrimPrefix(t.working.formatLine(t.working.Ended, config.Theme{}, false), "✓ ")
			return
		}
	}
}

func (t *tab) dismissCompletedWorking() {
	if !t.working.Active {
		t.working.reset()
	}
}

// Only this method feeds styled, trusted foreground sequences to the status row.
func (m *Model) workingLine(t *tab) string {
	line := t.working.line(time.Now(), m.cfg.Theme)
	if t.working.Active {
		line = render.AnimateWorkingLine(line, m.cfg.Theme, m.frame)
	}
	if t.readOnly() {
		status := render.Clean(t.ownershipStatus())
		if t.cycle.presence.Busy && !t.cycle.presence.Uncertain && t.cycle.probeErr == nil {
			line = render.AnimateWorkingLine(status, m.cfg.Theme, m.frame)
		} else {
			line = m.paint(status, m.cfg.Theme.Muted)
		}
	} else if m.status(t) == "working" {
		// Show the first-turn placeholder immediately for a submitted prompt;
		// native events still own the actual counters, usage and timing.
		if !t.working.Active {
			label := "working"
			if t.resume.submit != nil || t.sending || t.awaitingStart {
				label = "1 turns · 0s"
			}
			line = render.AnimateWorkingLine(label, m.cfg.Theme, m.frame)
		}
	} else if line == "" || t.dead || t.state.Compacting || t.sending || len(t.dialogs) > 0 {
		status := m.status(t)
		if status == "Ready" || status == "ready" || strings.EqualFold(status, "queued") || strings.EqualFold(status, "idle") {
			status = ""
		}
		line = ""
		if status != "" {
			line = m.paint(render.Clean(status), m.cfg.Theme.Muted)
		}
	} else if t.status != "" && t.status != "Ready" && !strings.EqualFold(t.status, "queued") && !strings.EqualFold(t.status, "idle") {
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
