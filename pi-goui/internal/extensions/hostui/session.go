package hostui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender/cards"
	"pi-goui/internal/extensions/worker"
	"time"
)

// Session owns optional per-session presentation, never backend configuration.
type Session struct {
	Codex CodexState
	Plan  PlanView
}
type PlanView struct {
	Scroll   int
	Revision uint64
	Expiry   PlanExpiry
}
type PlanExpiry struct {
	Initialized bool
	Revision    uint64
	File        string
	Generation  uint64
	Hidden      bool
}
type PlanExpired struct {
	ID         int
	Generation uint64
}

func SessionMessageID(msg tea.Msg) (int, bool) {
	switch v := msg.(type) {
	case CodexResult:
		return v.ID, true
	case PlanExpired:
		return v.ID, true
	}
	return 0, false
}
func (s *Session) Update(msg tea.Msg, state session.State, dead bool, notify func(string)) {
	switch v := msg.(type) {
	case CodexResult:
		s.Codex.Apply(v, dead, notify)
	case PlanExpired:
		e := &s.Plan.Expiry
		if e.Generation == v.Generation && e.Revision == state.PlanRevision && e.File == state.File {
			e.Hidden = true
		}
	}
}
func (s *Session) Sync(id int, state session.State) tea.Cmd {
	e := &s.Plan.Expiry
	if e.Initialized && e.Revision == state.PlanRevision && e.File == state.File {
		return nil
	}
	e.Initialized = true
	e.Revision, e.File = state.PlanRevision, state.File
	e.Generation++
	e.Hidden = false
	if len(state.Plan) == 0 {
		return nil
	}
	for _, step := range state.Plan {
		if step.Status != "completed" {
			return nil
		}
	}
	generation := e.Generation
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return PlanExpired{ID: id, Generation: generation} })
}
func (s *Session) Sidebar(ctx *render.SidebarContext, state session.State, dead bool) {
	if mode := s.Codex.Mode(dead); mode != "" {
		ctx.Mode = cards.CodexModeLabel(mode)
	}
	e := s.Plan.Expiry
	if !e.Hidden || e.Revision != state.PlanRevision || e.File != state.File {
		ctx.Todo = state.Plan
	}
	if s.Plan.Revision == state.PlanRevision {
		ctx.TodoScroll = s.Plan.Scroll
	}
}
func (s *Session) Scroll(state session.State, layout render.SidebarLayout, y, delta int) bool {
	if layout.TodoCount <= 0 || y < layout.TodoRow || y >= layout.TodoStart+layout.TodoCount {
		return false
	}
	offset := min(max(0, layout.TodoOffset+delta), max(0, len(state.Plan)-layout.TodoCount))
	s.Plan.Scroll = max(0, len(state.Plan)-layout.TodoCount-offset)
	s.Plan.Revision = state.PlanRevision
	return true
}

// Usage consumes only authoritative extension results; the host adds these
// neutral counters to its ordinary assistant usage without knowing tool names.
type Usage struct{ tracker worker.UsageTracker }

func (u *Usage) Observe(raw json.RawMessage) {
	var e struct {
		Type, ToolCallID               string
		PartialResult, Result, Message json.RawMessage
	}
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	switch e.Type {
	case "tool_execution_update":
		u.tracker.Apply(e.PartialResult, e.ToolCallID)
	case "tool_execution_end":
		u.tracker.Apply(e.Result, e.ToolCallID)
	case "message_end":
		u.tracker.Apply(e.Message, "")
	}
}
func (u *Usage) Totals() (float64, float64) {
	var input, output float64
	for _, v := range u.tracker {
		input += v.Input
		output += v.Output
	}
	return input, output
}
