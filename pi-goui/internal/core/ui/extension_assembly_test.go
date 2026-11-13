package ui

import (
	"encoding/json"
	"testing"
)

func TestEveryTabGetsIndependentExtensionProjection(t *testing.T) {
	m := testModel(t, "{}")
	first := m.current()
	second := m.addTab("")
	first.state.Apply(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"plan","toolName":"update_plan","result":{"details":{"plan":[{"id":"a","goal":"first tab only","status":"pending"}]}}}`))
	if len(first.state.Plan) != 1 || len(second.state.Plan) != 0 {
		t.Fatal("tab creation omitted projection or shared mutable state")
	}
	second.state.Apply(json.RawMessage(`{"type":"tool_execution_end","toolCallId":"plan","toolName":"update_plan","result":{"details":{"plan":[{"id":"b","goal":"second tab only","status":"completed"}]}}}`))
	if first.state.Plan[0].ID != "a" || len(second.state.Plan) != 1 || second.state.Plan[0].ID != "b" {
		t.Fatal("background and active tab projections are not independent")
	}
}
