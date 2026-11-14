// Package sessionview composes optional, read-only extension projections.
// It accepts native protocol data, never imports the host session package and
// never reads extension files or changes the backend's state.
package sessionview

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
	"slices"

	"pi-goui/internal/extensions/solpi"
	"pi-goui/internal/extensions/web"
)

// Message is the native message metadata needed by optional projections.
type Message = extensionapi.SessionMessage

// Entry is native entry metadata, independent of the host's history types.
type Entry = extensionapi.SessionEntry

// State belongs to one session. Plan and PlanRevision remain display snapshots;
// reconciliation cursors are private and cannot be changed by consumers.
type State struct {
	Plan                []solpi.Step
	PlanRevision        uint64
	liveEntry, liveTool string
}

// Clone transfers no mutable projection storage to a background replay owner.
func (s *State) Clone() *State {
	if s == nil {
		return &State{}
	}
	next := *s
	next.Plan = slices.Clone(s.Plan)
	return &next
}

func (s *State) setPlan(steps []solpi.Step) {
	if len(s.Plan) == len(steps) {
		equal := true
		for i := range steps {
			if steps[i] != s.Plan[i] {
				equal = false
				break
			}
		}
		if equal {
			return
		}
	}
	s.Plan = steps
	s.PlanRevision++
}

var _ extensionapi.SessionProjection = (*State)(nil)

func (s *State) View() extensionapi.SessionView {
	return extensionapi.SessionView{Plan: s.Plan, PlanRevision: s.PlanRevision}
}
func (s *State) HideMessage(role, customType string) bool { return HideMessage(role, customType) }

func (s *State) Reset() {
	s.setPlan(nil)
	s.liveEntry, s.liveTool = "", ""
}

func planFromMessage(m Message) ([]solpi.Step, bool) {
	if m.Role != "toolResult" || m.ToolName != solpi.UpdatePlanTool || (m.IsError != nil && *m.IsError) {
		return nil, false
	}
	return solpi.ParsePlan(m.Details)
}

// ObserveMessage receives a complete message before host deduplication, just as
// execution events may arrive before their persisted tool result.
func (s *State) ObserveMessage(m Message, finished bool) {
	if !finished {
		return
	}
	if steps, ok := planFromMessage(m); ok {
		revision := s.PlanRevision
		s.setPlan(steps)
		if s.liveEntry == "" && s.PlanRevision != revision {
			s.liveTool = m.ToolCallID
		}
	}
}

func (s *State) ObserveEvent(raw json.RawMessage) {
	var event struct {
		Type, ToolName, ToolCallID string
		Entry                      Entry
		IsError                    *bool
		Result                     json.RawMessage
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	switch event.Type {
	case "entry_appended":
		if steps, ok := solpi.PlanFromEntry(event.Entry.Type, event.Entry.CustomType, event.Entry.Data); ok {
			s.setPlan(steps)
			s.liveEntry, s.liveTool = event.Entry.ID, ""
		}
	case "tool_execution_end":
		if event.ToolName != solpi.UpdatePlanTool || (event.IsError != nil && *event.IsError) {
			return
		}
		var result Message
		if json.Unmarshal(event.Result, &result) != nil {
			return
		}
		result.Role, result.ToolName, result.ToolCallID = "toolResult", event.ToolName, event.ToolCallID
		s.ObserveMessage(result, true)
	}
}

// ObserveFallback only supplements sessions for which native full history has
// not been loaded. It must not roll back a live update awaiting persistence.
func (s *State) ObserveFallback(messages []Message, hasHistory bool) {
	if hasHistory || s.liveEntry != "" || s.liveTool != "" {
		return
	}
	var latest []solpi.Step
	found := false
	for _, m := range messages {
		if steps, ok := planFromMessage(m); ok {
			latest, found = steps, true
		}
	}
	if found {
		s.setPlan(latest)
	}
}

// Snapshot is a provisional active-branch view. History errors discard it;
// Commit reconciles it with live state only after native history validation.
type Snapshot struct {
	owner               *State
	plan                []solpi.Step
	liveEntry, liveTool string
	caughtUp            bool
	metrics             map[string]*solpi.CompactionMetrics
}

// NewSnapshot takes active ancestry in newest-first order. It collects metadata
// before rendering older checkpoints, retaining the newest annotation per ID.
func (s *State) NewSnapshot(ancestry []Entry) extensionapi.SessionSnapshot {
	p := &Snapshot{owner: s, liveEntry: s.liveEntry, liveTool: s.liveTool,
		caughtUp: s.liveEntry == "" && s.liveTool == "", metrics: map[string]*solpi.CompactionMetrics{}}
	for _, e := range ancestry {
		if id, value := solpi.MetricsFromEntry(e.Type, e.CustomType, e.Data); value != nil && p.metrics[id] == nil {
			p.metrics[id] = value
		}
	}
	return p
}

// ObserveEntry and ObserveMessage are fed in chronological order, using the
// host's reconciled message metadata (which can restore omitted live details).
func (p *Snapshot) ObserveEntry(e Entry) {
	if steps, ok := solpi.PlanFromEntry(e.Type, e.CustomType, e.Data); ok {
		p.plan = steps
	}
	if e.ID == p.liveEntry {
		p.caughtUp = true
	}
}

func (p *Snapshot) ObserveMessage(m Message) {
	if m.Role != "toolResult" {
		return
	}
	if m.ToolCallID == p.liveTool {
		p.caughtUp = true
	}
	if steps, ok := planFromMessage(m); ok {
		p.plan = steps
	}
}

func (p *Snapshot) Compaction(id string) *solpi.CompactionMetrics { return p.metrics[id] }

func (s *State) Commit(snapshot extensionapi.SessionSnapshot, branchChanged bool) {
	p, ok := snapshot.(*Snapshot)
	if !ok || p == nil || p.owner != s {
		return
	}
	if branchChanged || p.caughtUp {
		s.setPlan(p.plan)
		s.liveEntry, s.liveTool = "", ""
	}
}

// Hidden messages still participate in native record counting and history.
func HideMessage(role, customType string) bool { return web.HideMessage(role, customType) }
