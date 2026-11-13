package sessionview

import (
	"encoding/json"
	"testing"
)

func TestPlanResultValidationAndReplacement(t *testing.T) {
	var s State
	const pendingPlan = `[{"id":"a","goal":"Inspect","status":"pending"}]`
	const donePlan = `[{"id":"a","goal":"Inspect","status":"completed"}]`
	s.ObserveEvent(json.RawMessage(`{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"one","result":{"details":{"plan":` + pendingPlan + `}}}`))
	if len(s.Plan) != 1 {
		t.Fatal("missing direct plan")
	}
	for _, event := range []string{
		`{"type":"tool_execution_end","toolName":"update_plan","isError":true,"result":{"details":{"plan":` + donePlan + `}}}`,
		`{"type":"tool_execution_end","toolName":"update_plan","result":{"isError":true,"details":{"plan":` + donePlan + `}}}`,
		`{"type":"tool_execution_update","toolName":"update_plan","partialResult":{"details":{"plan":` + donePlan + `}}}`,
		`{"type":"tool_execution_end","toolName":"other","result":{"details":{"plan":` + donePlan + `}}}`,
		`{"type":"entry_appended","entry":{"type":"custom","customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":[{"id":"b","goal":"bad","status":"unknown"}]}}}`,
	} {
		s.ObserveEvent(json.RawMessage(event))
		if s.Plan[0].Status != "pending" {
			t.Fatal("invalid event changed plan", event)
		}
	}
	s.ObserveMessage(Message{Role: "toolResult", ToolName: "update_plan", ToolCallID: "two", Details: json.RawMessage(`{"plan":[{"id":"new","goal":"New task","status":"in_progress"}]}`)}, true)
	if len(s.Plan) != 1 || s.Plan[0].ID != "new" {
		t.Fatal("plan was not replaced")
	}
	s.ObserveEvent(json.RawMessage(`{"type":"entry_appended","entry":{"type":"custom","id":"empty","customType":"sol-pi-online-context-state-v1","data":{"version":1,"plan":[]}}}`))
	if len(s.Plan) != 0 {
		t.Fatal("empty authoritative plan did not clear")
	}
}
