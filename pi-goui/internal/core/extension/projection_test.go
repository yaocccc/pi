package extension

import (
	"encoding/json"
	"pi-goui/internal/core/extensionapi"
	"testing"
)

type projectionStub struct {
	plan                                 []extensionapi.PlanStep
	events, messages, fallbacks, commits int
	hidden                               bool
	metrics                              *extensionapi.CompactionMetrics
}

func (p *projectionStub) View() extensionapi.SessionView {
	return extensionapi.SessionView{Plan: p.plan}
}
func (p *projectionStub) Reset()                                              { p.plan = nil }
func (p *projectionStub) ObserveEvent(json.RawMessage)                        { p.events++ }
func (p *projectionStub) ObserveMessage(extensionapi.SessionMessage, bool)    { p.messages++ }
func (p *projectionStub) ObserveFallback([]extensionapi.SessionMessage, bool) { p.fallbacks++ }
func (p *projectionStub) HideMessage(string, string) bool                     { return p.hidden }
func (p *projectionStub) NewSnapshot([]extensionapi.SessionEntry) extensionapi.SessionSnapshot {
	return &snapshotStub{metrics: p.metrics}
}
func (p *projectionStub) Commit(s extensionapi.SessionSnapshot, _ bool) {
	p.commits++
	p.plan = s.(*snapshotStub).plan
}

type snapshotStub struct {
	plan     []extensionapi.PlanStep
	metrics  *extensionapi.CompactionMetrics
	messages int
}

func (s *snapshotStub) ObserveEntry(e extensionapi.SessionEntry) {
	s.plan = append(s.plan, extensionapi.PlanStep{ID: e.ID, Goal: "history"})
}
func (s *snapshotStub) ObserveMessage(extensionapi.SessionMessage)        { s.messages++ }
func (s *snapshotStub) Compaction(string) *extensionapi.CompactionMetrics { return s.metrics }

func TestCompositeProjectionLifecycleAndSnapshotOwnership(t *testing.T) {
	var instances []*projectionStub
	makeProvider := func(hidden bool) Extension {
		return contribution{Capabilities{NewProjection: func() extensionapi.SessionProjection {
			p := &projectionStub{hidden: hidden}
			if hidden {
				p.metrics = &extensionapi.CompactionMetrics{BeforeTokens: 100, AfterTokens: 20}
			}
			instances = append(instances, p)
			return p
		}}}
	}
	r := New([]Extension{makeProvider(false), makeProvider(true)})
	a, b := r.NewProjection(), r.NewProjection()
	if len(instances) != 4 || a == b {
		t.Fatal("projection factories were shared")
	}
	a.ObserveEvent(nil)
	a.ObserveMessage(extensionapi.SessionMessage{}, true)
	a.ObserveFallback(nil, false)
	for i, p := range instances {
		want := 0
		if i < 2 {
			want = 1
		}
		if p.events != want || p.messages != want || p.fallbacks != want {
			t.Fatal("observation leaked or missed provider")
		}
	}
	if !a.HideMessage("custom", "private") {
		t.Fatal("later visibility predicate ignored")
	}
	instances[1].plan = []extensionapi.PlanStep{{ID: "second"}}
	first := a.View()
	if len(first.Plan) != 1 || first.Plan[0].ID != "second" {
		t.Fatal("later plan ignored")
	}
	first.Plan[0].ID = "consumer mutation"
	if a.View().Plan[0].ID != "second" {
		t.Fatal("display snapshot aliases runtime")
	}
	instances[0].plan = []extensionapi.PlanStep{{ID: "first"}}
	next := a.View()
	if next.Plan[0].ID != "first" || next.PlanRevision <= first.PlanRevision {
		t.Fatal("priority or revision lost")
	}
	instances[0].plan = nil
	if got := a.View(); got.Plan[0].ID != "second" || got.PlanRevision <= next.PlanRevision {
		t.Fatal("cleared provider did not reveal next plan")
	}
	pending := a.NewSnapshot(nil)
	pending.ObserveEntry(extensionapi.SessionEntry{ID: "loaded"})
	pending.ObserveMessage(extensionapi.SessionMessage{})
	if a.View().Plan[0].ID != "second" {
		t.Fatal("provisional snapshot polluted live plan")
	}
	if metrics := pending.Compaction("entry"); metrics == nil || metrics.AfterTokens != 20 {
		t.Fatal("later snapshot metrics ignored")
	} else {
		metrics.AfterTokens = 999
	}
	if pending.Compaction("entry").AfterTokens != 20 {
		t.Fatal("consumer mutated snapshot metrics")
	}
	b.Commit(pending, true)
	if instances[2].commits != 0 || instances[3].commits != 0 {
		t.Fatal("foreign snapshot committed")
	}
	a.Commit(pending, true)
	for _, p := range instances[:2] {
		if p.commits != 1 || len(p.plan) != 1 || p.plan[0].ID != "loaded" {
			t.Fatal("commit not delivered to owner")
		}
	}
	a.Reset()
	if len(a.View().Plan) != 0 || len(b.View().Plan) != 0 {
		t.Fatal("reset did not clear projections")
	}
}
