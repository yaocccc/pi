package ui

import (
	"testing"
	"time"

	"pi-goui/internal/session"
)

func TestAssistantStatsFreezeWorkingMetrics(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	now := time.Unix(1700000000, 0)
	tab.state.Items = []session.Item{{ID: "old", Role: "assistant", Text: "previous reply"}}
	tab.workingStart = len(tab.state.Items)
	workingApply(&tab.working, `{"type":"agent_start"}`, now)
	workingApply(&tab.working, workingFinal(1, 4), now.Add(time.Second))
	tab.state.Items = append(tab.state.Items, session.Item{ID: "new", Role: "assistant", Text: "completed"})
	tab.saveAssistantStats()
	if len(tab.assistantStats) != 0 {
		t.Fatal("saved before completion")
	}
	workingApply(&tab.working, `{"type":"agent_settled"}`, now.Add(2*time.Second))
	tab.saveAssistantStats()
	want := "↑35 ↓4 · 2s"
	if tab.assistantStats["new"] != want || tab.assistantStats["old"] != "" {
		t.Fatalf("incorrect metrics: %+v", tab.assistantStats)
	}
	tab.dismissCompletedWorking()
	if tab.assistantStats["new"] != want {
		t.Fatal("dismiss erased metrics")
	}
	tab.workingStart = len(tab.state.Items)
	workingApply(&tab.working, `{"type":"agent_start"}`, now.Add(time.Minute))
	workingApply(&tab.working, `{"type":"agent_settled"}`, now.Add(2*time.Minute))
	tab.saveAssistantStats()
	if tab.assistantStats["new"] != want {
		t.Fatal("empty run overwrote previous metrics")
	}
}
