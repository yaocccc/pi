package session

import (
	"encoding/json"
	"pi-goui/internal/extensions/sessionview"
	"testing"
)

const pendingPlan = `[{"id":"a","goal":"Inspect","status":"pending"}]`
const donePlan = `[{"id":"a","goal":"Inspect","status":"completed"}]`

func planEntry(id, parent, plan string) string {
	return `{"id":"` + id + `","parentId":` + parent + `,"type":"custom","customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":` + plan + `}}`
}
func TestPlanLiveNestedAndHistory(t *testing.T) {
	var s State
	s.SetExtensionProjection(&sessionview.State{})
	old := planEntry("old", "null", pendingPlan)
	if err := s.LoadEntries(json.RawMessage(`{"entries":[` + old + `],"leafId":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 1 || s.Plan[0].Status != "pending" {
		t.Fatal(s.Plan)
	}
	latest := planEntry("latest", `"old"`, donePlan)
	s.Apply(json.RawMessage(`{"type":"entry_appended","entry":` + latest + `}`))
	revision := s.PlanRevision
	// A nested update is visible without leaking a child into the transcript.
	s.Apply(json.RawMessage(`{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"child","parentToolCallId":"parent","result":{"details":{"plan":` + donePlan + `}}}`))
	if len(s.Items) != 0 || s.Plan[0].Status != "completed" || s.PlanRevision != revision {
		t.Fatalf("nested: %#v", s)
	}
	// An in-flight old refresh must not roll back the live update.
	if err := s.LoadEntries(json.RawMessage(`{"entries":[` + old + `],"leafId":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if s.Plan[0].Status != "completed" {
		t.Fatal("stale refresh replaced live plan")
	}
	compact := `{"type":"compaction","id":"compact","parentId":"latest","summary":"summary"}`
	snapshot := json.RawMessage(`{"entries":[` + old + `,` + latest + `,` + compact + `],"leafId":"compact"}`)
	if err := s.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	var restored State
	restored.SetExtensionProjection(&sessionview.State{})
	if err := restored.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(restored.Plan) != 1 || restored.Plan[0].Status != "completed" {
		t.Fatal("restore lost plan")
	}
	// Explicit branch navigation clears a plan not present on the target branch.
	if err := s.LoadEntries(json.RawMessage(`{"entries":[{"type":"message","id":"other","message":{"role":"user","content":"new branch"}}],"leafId":"other"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 0 {
		t.Fatal("plan leaked across branches")
	}
}

func TestPlanSessionResetAndMessageFallback(t *testing.T) {
	var s State
	s.SetExtensionProjection(&sessionview.State{})
	if err := s.LoadState(json.RawMessage(`{"sessionId":"first"}`)); err != nil {
		t.Fatal(err)
	}
	messages := json.RawMessage(`{"messages":[{"role":"toolResult","toolName":"update_plan","toolCallId":"a","details":{"plan":` + pendingPlan + `}},{"role":"toolResult","toolName":"update_plan","toolCallId":"b","details":{"plan":` + donePlan + `}}]}`)
	if err := s.LoadMessages(messages); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 1 || s.Plan[0].Status != "completed" {
		t.Fatal("fallback missing latest plan")
	}
	revision := s.PlanRevision
	if err := s.LoadMessages(messages); err != nil {
		t.Fatal(err)
	}
	if s.PlanRevision != revision {
		t.Fatal("repeated fallback changed revision")
	}
	s.Apply(json.RawMessage(`{"type":"entry_appended","entry":` + planEntry("live", "null", pendingPlan) + `}`))
	if err := s.LoadState(json.RawMessage(`{"sessionId":"second"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 0 {
		t.Fatal("new session retained plan")
	}
	if err := s.LoadMessages(messages); err != nil {
		t.Fatal(err)
	}
	if len(s.Plan) != 1 || s.Plan[0].Status != "completed" {
		t.Fatal("old reconciliation cursor blocked the new session fallback")
	}
}
