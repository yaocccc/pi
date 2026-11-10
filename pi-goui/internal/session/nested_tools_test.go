package session

import "testing"

func TestNestedToolEventsDoNotLeakIntoLaterTurns(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"tool_execution_start","toolCallId":"parent","toolName":"codemode"}`)
	for _, event := range []string{
		`{"type":"tool_execution_start","toolCallId":"child","parentToolCallId":"parent","toolName":"read"}`,
		`{"type":"tool_execution_update","toolCallId":"child","parentToolCallId":"parent","toolName":"read","partialResult":{"content":[{"type":"text","text":"partial"}]}}`,
		`{"type":"tool_execution_end","toolCallId":"child","parentToolCallId":"parent","toolName":"read","result":{"content":[{"type":"text","text":"done"}]}}`,
	} {
		apply(&s, event)
		if len(s.Items) != 1 || s.Items[0].ToolID != "parent" || len(s.tools) != 1 || len(s.toolIDs) != 1 {
			t.Fatalf("nested event created top-level tools: %+v", s.Items)
		}
	}
	apply(&s, `{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"parent","name":"codemode","arguments":{}}]}}`)
	apply(&s, `{"type":"tool_execution_end","toolCallId":"parent","toolName":"codemode","result":{"content":[{"type":"text","text":"done"}],"details":{"calls":[{"name":"read"}]}}}`)
	apply(&s, `{"type":"agent_settled"}`)
	apply(&s, `{"type":"message_end","message":{"role":"user","content":"next question"}}`)
	apply(&s, `{"type":"agent_start"}`)
	if len(s.Items) != 2 || s.Items[1].Role != "user" {
		t.Fatalf("old nested tools appeared after new question: %+v", s.Items)
	}
	if len(s.Items[0].Details) == 0 {
		t.Fatal("parent lost nested call details")
	}
	apply(&s, `{"type":"tool_execution_start","toolCallId":"next","toolName":"bash"}`)
	if len(s.Items) != 3 || s.Items[2].ToolID != "next" {
		t.Fatal("top-level early tool start was lost")
	}
}
