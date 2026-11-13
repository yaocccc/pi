package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolItem(t *testing.T, s *State, id string) Item {
	t.Helper()
	var found []Item
	for _, it := range s.Items {
		if it.ToolID == id {
			found = append(found, it)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one tool %q, got %#v", id, s.Items)
	}
	return found[0]
}
func loadToolHistory(t *testing.T, s *State, details string) {
	t.Helper()
	raw := `{"entries":[{"id":"a","type":"message","parentId":null,"message":{"role":"assistant","content":[{"type":"toolCall","id":"edit1","name":"edit","arguments":{"path":"main.go","edits":[{"oldText":"a","newText":"b"}]}}]}},{"id":"b","type":"message","parentId":"a","message":{"role":"toolResult","toolCallId":"edit1","toolName":"edit","content":[{"type":"text","text":"saved"}]` + details + `}}],"leafId":"b"}`
	if err := s.LoadEntries([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}
func TestToolHistoryDetailsAndFallback(t *testing.T) {
	var s State
	loadToolHistory(t, &s, `,"details":{"diff":"-1 a\n+1 b","patch":"@@ -1 +1 @@\n-a\n+b","firstChangedLine":1}`)
	it := toolItem(t, &s, "edit1")
	if !it.HasResult || it.Running || it.Error || !strings.Contains(string(it.Details), "firstChangedLine") || !strings.Contains(string(it.Arguments), "edits") {
		t.Fatalf("lost semantic fields: %#v", it)
	}
	before := it.EstimatedTokens
	if before == 0 {
		t.Fatal("missing estimate")
	}
	loadToolHistory(t, &s, "")
	it = toolItem(t, &s, "edit1")
	if !strings.Contains(string(it.Details), "patch") || it.EstimatedTokens != before {
		t.Fatal("refresh erased details or counted details as payload")
	}
	loadToolHistory(t, &s, `,"details":null`)
	if string(toolItem(t, &s, "edit1").Details) != "null" {
		t.Fatal("explicit null must clear details")
	}
	var fallback State
	if err := fallback.LoadMessages([]byte(`{"messages":[{"role":"toolResult","toolCallId":"x","content":"legacy","details":{"diff":"+1 legacy"},"isError":true}]}`)); err != nil {
		t.Fatal(err)
	}
	it = toolItem(t, &fallback, "x")
	if !it.HasResult || !it.Error || !strings.Contains(string(it.Details), "legacy") {
		t.Fatalf("fallback lost details: %#v", it)
	}
}
func TestToolPartialFinalAndMessageProjection(t *testing.T) {
	var s State
	apply(&s, `{"type":"tool_execution_start","toolCallId":"edit1","toolName":"edit","args":{"path":"main.go"}}`)
	if it := toolItem(t, &s, "edit1"); it.HasResult || !it.Running {
		t.Fatal("bare call marked as result")
	}
	apply(&s, `{"type":"tool_execution_update","toolCallId":"edit1","partialResult":{"content":[{"type":"text","text":"partial"}],"details":{"diff":"+1 partial"},"isError":true}}`)
	it := toolItem(t, &s, "edit1")
	if !it.HasResult || !it.Running || !it.Error {
		t.Fatalf("partial: %#v", it)
	}
	apply(&s, `{"type":"tool_execution_end","toolCallId":"edit1","result":{"content":[{"type":"text","text":"final"}],"details":{"diff":"+1 final","patch":"patch"},"isError":true},"isError":false}`)
	it = toolItem(t, &s, "edit1")
	if it.Running || it.Error || it.Text != "final" || !strings.Contains(string(it.Arguments), "main.go") {
		t.Fatalf("final/outer error: %#v", it)
	}
	apply(&s, `{"type":"tool_execution_update","toolCallId":"edit1","partialResult":{"content":"late partial","details":{"diff":"old"}}}`)
	if it = toolItem(t, &s, "edit1"); it.Running || it.Text != "final" || !strings.Contains(string(it.Details), "patch") {
		t.Fatal("late partial regressed final")
	}
	apply(&s, `{"type":"message_end","message":{"role":"toolResult","toolCallId":"edit1","content":[]}}`)
	it = toolItem(t, &s, "edit1")
	if it.Text != "" || !it.HasResult || !strings.Contains(string(it.Details), "patch") {
		t.Fatalf("empty final must replace content but retain omitted details: %#v", it)
	}
	apply(&s, `{"type":"agent_settled"}`)
	loadToolHistory(t, &s, `,"details":{"diff":"saved diff"}`)
	it = toolItem(t, &s, "edit1")
	if it.Text != "saved" || !strings.Contains(string(it.Details), "saved diff") || !strings.Contains(string(it.Arguments), "edits") {
		t.Fatalf("saved history not authoritative: %#v", it)
	}
}
func TestToolMessageOnlyAndMissingFinalFields(t *testing.T) {
	var s State
	apply(&s, `{"type":"message_start","message":{"role":"toolResult","toolCallId":"x","content":"starting","details":{"diff":"partial"}}}`)
	if !toolItem(t, &s, "x").Running {
		t.Fatal("message_start incorrectly final")
	}
	apply(&s, `{"type":"message_end","message":{"role":"toolResult","toolCallId":"x","content":"complete","details":{"diff":"complete"},"isError":true}}`)
	apply(&s, `{"type":"message_start","message":{"role":"toolResult","toolCallId":"x","content":"late"}}`)
	it := toolItem(t, &s, "x")
	if it.Running || !it.Error || it.Text != "complete" {
		t.Fatalf("message result regressed: %#v", it)
	}
	var exec State
	apply(&exec, `{"type":"tool_execution_update","toolCallId":"x","partialResult":{"content":"known","details":{"diff":"known"},"isError":true}}`)
	apply(&exec, `{"type":"tool_execution_end","toolCallId":"x","result":{}}`)
	apply(&exec, `{"type":"message_end","message":{"role":"toolResult","toolCallId":"x"}}`)
	it = toolItem(t, &exec, "x")
	if it.Text != "known" || !it.Error || !strings.Contains(string(it.Details), "known") || it.Running {
		t.Fatalf("omitted final fields lost: %#v", it)
	}
}
func TestHistoricalToolTokenEstimateUTF16AndImages(t *testing.T) {
	var s State
	apply(&s, `{"type":"tool_execution_start","toolCallId":"x","args":{"path":"\ud83d\ude00中"}}`)
	apply(&s, `{"type":"tool_execution_end","toolCallId":"x","result":{"content":[{"type":"text","text":"😀"},{"type":"text","text":"中"},{"type":"image","mimeType":"image/png","data":"AAAA"}],"details":{"huge":"this must not count"}}}`)
	it := toolItem(t, &s, "x")
	if it.EstimatedTokens != 1205 {
		t.Fatalf("expected ceil(14/4)+ceil(4803/4)=1205, got %d", it.EstimatedTokens)
	}
	if strings.Contains(it.Text, "AAAA") || !strings.Contains(it.Text, "[image]") {
		t.Fatal("image must retain placeholder, not base64")
	}
	var saved State
	if err := saved.LoadMessages([]byte(`{"messages":[{"role":"assistant","content":[{"type":"toolCall","id":"x","name":"read","arguments":{"path":"😀中"}}]},{"role":"toolResult","toolCallId":"x","content":[{"type":"text","text":"😀"},{"type":"text","text":"中"},{"type":"image","mimeType":"image/png","data":"AAAA"}],"details":{"mimeType":"image/png"}}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := toolItem(t, &saved, "x"); got.EstimatedTokens != it.EstimatedTokens || !strings.Contains(string(got.Details), "image/png") {
		t.Fatalf("history/live estimate mismatch: %#v", got)
	}
	for raw, want := range map[string]int{`{}`: 2, `null`: 2, `{"s":"<>&\u2028😀"}`: 14, `{"n":1e20}`: 27, `[true,false,null]`: 17, `{"s":"\n\u000b"}`: 16} {
		if got := argumentChars(json.RawMessage(raw)); got != want {
			t.Errorf("stringify length %s: %d != %d", raw, got, want)
		}
	}
	var bash State
	if err := bash.LoadMessages([]byte(`{"messages":[{"role":"bashExecution","command":"echo 😀","output":"ok\n","exitCode":7}]}`)); err != nil {
		t.Fatal(err)
	}
	it = bash.Items[0]
	if it.EstimatedTokens != 3 || !it.HasResult || !it.Error || !strings.Contains(string(it.Details), "7") {
		t.Fatalf("bash estimate/details: %#v", it)
	}
}

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
