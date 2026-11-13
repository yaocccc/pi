package session

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
)

// Optional projection state contains only neutral display data and an
// instance-owned capability. A zero State has no optional projections.
type extensionState struct {
	Plan         []PlanStep
	PlanRevision uint64
	projection   extensionapi.SessionProjection
}
type PlanStep = extensionapi.PlanStep

// SetExtensionProjection binds an optional projector before history is loaded.
// A projector must not be shared between states. Replacing a projector requires
// reloading history to populate its derived state; nil disables projection.
func (s *State) SetExtensionProjection(p extensionapi.SessionProjection) {
	s.projection = p
	s.syncExtensionView()
	s.project()
}
func (s *State) syncExtensionView() {
	if s.projection == nil {
		s.Plan = nil
		s.PlanRevision = 0
		return
	}
	v := s.projection.View()
	s.Plan = append([]PlanStep(nil), v.Plan...)
	s.PlanRevision = v.PlanRevision
}
func (s *extensionState) Reset() {
	if s.projection != nil {
		s.projection.Reset()
		v := s.projection.View()
		s.Plan = append([]PlanStep(nil), v.Plan...)
		s.PlanRevision = v.PlanRevision
	} else {
		s.Plan = nil
		s.PlanRevision = 0
	}
}
func (s *State) observeExtensionEvent(raw json.RawMessage) {
	if s.projection != nil {
		s.projection.ObserveEvent(raw)
		s.syncExtensionView()
	}
}
func projectionEntry(e entry) extensionapi.SessionEntry {
	return extensionapi.SessionEntry{Type: e.Type, ID: e.ID, CustomType: e.CustomType, Data: e.Data}
}
func projectionMessage(m message) extensionapi.SessionMessage {
	return extensionapi.SessionMessage{Role: m.Role, ToolName: m.ToolName, ToolCallID: m.ToolCallID, Details: m.Details, IsError: m.IsError}
}

type extensionSnapshot struct{ snapshot extensionapi.SessionSnapshot }

func (s *State) newExtensionSnapshot(path []entry) extensionSnapshot {
	if s.projection == nil {
		return extensionSnapshot{}
	}
	entries := make([]extensionapi.SessionEntry, len(path))
	for i, e := range path {
		entries[i] = projectionEntry(e)
	}
	return extensionSnapshot{s.projection.NewSnapshot(entries)}
}
func (p extensionSnapshot) observeEntry(e entry) {
	if p.snapshot != nil {
		p.snapshot.ObserveEntry(projectionEntry(e))
	}
}
func (p extensionSnapshot) observeMessage(m message) {
	if p.snapshot != nil {
		p.snapshot.ObserveMessage(projectionMessage(m))
	}
}
func (p extensionSnapshot) compaction(id string) *CompactionMetrics {
	if p.snapshot != nil {
		if metrics := p.snapshot.Compaction(id); metrics != nil {
			return &CompactionMetrics{BeforeTokens: metrics.BeforeTokens, AfterTokens: metrics.AfterTokens}
		}
	}
	return nil
}
func (s *State) commitExtensionSnapshot(p extensionSnapshot, branchChanged bool) {
	if s.projection != nil && p.snapshot != nil {
		s.projection.Commit(p.snapshot, branchChanged)
		s.syncExtensionView()
	}
}
func (s *State) observeExtensionMessage(m message, finished bool) {
	if s.projection != nil {
		s.projection.ObserveMessage(projectionMessage(m), finished)
		s.syncExtensionView()
	}
}
func (s *State) observeExtensionFallback(messages []message) {
	if s.projection == nil {
		return
	}
	values := make([]extensionapi.SessionMessage, len(messages))
	for i, m := range messages {
		values[i] = projectionMessage(m)
	}
	s.projection.ObserveFallback(values, len(s.records) != 0)
	s.syncExtensionView()
}
func (s *State) hideExtensionMessage(m message) bool {
	return s.projection != nil && s.projection.HideMessage(m.Role, m.CustomType)
}
