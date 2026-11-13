package extension

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
	"slices"
)

func (r *Runtime) NewProjection() extensionapi.SessionProjection {
	var parts []extensionapi.SessionProjection
	for _, c := range r.capabilities {
		if c.NewProjection != nil {
			if p := c.NewProjection(); p != nil {
				parts = append(parts, p)
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return &projection{parts: parts}
}

// Observations and commits fan out. The first nonempty plan and first available
// metrics win; hidden-message predicates are ORed. Revision tracks the selected
// display plan, including changes in ownership after a provider clears it.
type projection struct {
	parts []extensionapi.SessionProjection
	view  extensionapi.SessionView
}

func (p *projection) View() extensionapi.SessionView {
	var plan []extensionapi.PlanStep
	for _, v := range p.parts {
		if candidate := v.View().Plan; len(candidate) > 0 {
			plan = candidate
			break
		}
	}
	if !slices.Equal(plan, p.view.Plan) {
		p.view.Plan = append([]extensionapi.PlanStep(nil), plan...)
		p.view.PlanRevision++
	}
	return extensionapi.SessionView{Plan: append([]extensionapi.PlanStep(nil), p.view.Plan...), PlanRevision: p.view.PlanRevision}
}
func (p *projection) Reset() {
	for _, v := range p.parts {
		v.Reset()
	}
}
func (p *projection) ObserveEvent(raw json.RawMessage) {
	for _, v := range p.parts {
		v.ObserveEvent(raw)
	}
}
func (p *projection) ObserveMessage(m extensionapi.SessionMessage, done bool) {
	for _, v := range p.parts {
		v.ObserveMessage(m, done)
	}
}
func (p *projection) ObserveFallback(m []extensionapi.SessionMessage, history bool) {
	for _, v := range p.parts {
		v.ObserveFallback(m, history)
	}
}
func (p *projection) HideMessage(role, kind string) bool {
	for _, v := range p.parts {
		if v.HideMessage(role, kind) {
			return true
		}
	}
	return false
}

type snapshot struct {
	owner *projection
	parts []extensionapi.SessionSnapshot
}

func (p *projection) NewSnapshot(entries []extensionapi.SessionEntry) extensionapi.SessionSnapshot {
	s := &snapshot{owner: p}
	for _, v := range p.parts {
		s.parts = append(s.parts, v.NewSnapshot(entries))
	}
	return s
}
func (p *projection) Commit(candidate extensionapi.SessionSnapshot, changed bool) {
	s, ok := candidate.(*snapshot)
	if !ok || s == nil || s.owner != p {
		return
	}
	for i, v := range p.parts {
		if s.parts[i] != nil {
			v.Commit(s.parts[i], changed)
		}
	}
}
func (s *snapshot) ObserveEntry(e extensionapi.SessionEntry) {
	for _, v := range s.parts {
		if v != nil {
			v.ObserveEntry(e)
		}
	}
}
func (s *snapshot) ObserveMessage(m extensionapi.SessionMessage) {
	for _, v := range s.parts {
		if v != nil {
			v.ObserveMessage(m)
		}
	}
}
func (s *snapshot) Compaction(id string) *extensionapi.CompactionMetrics {
	for _, v := range s.parts {
		if v != nil {
			if metrics := v.Compaction(id); metrics != nil {
				copy := *metrics
				return &copy
			}
		}
	}
	return nil
}
