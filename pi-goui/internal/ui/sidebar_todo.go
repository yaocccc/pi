package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type todoExpiryState struct {
	initialized bool
	revision    uint64
	file        string
	generation  uint64
	hidden      bool
}

type todoExpiredMsg struct {
	id         int
	generation uint64
}

// Only genuine plan/session changes restart the grace period. The generation
// guards against old timers, including when another plan replaces this one.
func (t *tab) syncTodoExpiry() tea.Cmd {
	s := &t.todoExpiry
	if s.initialized && s.revision == t.state.PlanRevision && s.file == t.state.File {
		return nil
	}
	s.initialized = true
	s.revision, s.file = t.state.PlanRevision, t.state.File
	s.generation++
	s.hidden = false
	if len(t.state.Plan) == 0 {
		return nil
	}
	for _, step := range t.state.Plan {
		if step.Status != "completed" {
			return nil
		}
	}
	id, generation := t.id, s.generation
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return todoExpiredMsg{id: id, generation: generation}
	})
}
