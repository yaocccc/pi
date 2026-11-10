package session

import (
	"encoding/json"
	"reflect"
	"testing"
)

func ordinalEntries(t *testing.T, s *State, entries []map[string]any, leaf any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"entries": entries, "leafId": leaf})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
}
func ordinalEntry(id string, parent any, raw string) map[string]any {
	return map[string]any{"type": "message", "id": id, "parentId": parent, "message": json.RawMessage(raw)}
}
func indices(s *State) []int {
	out := make([]int, len(s.Items))
	for i, item := range s.Items {
		out[i] = item.MessageIndex
	}
	return out
}
func requireIndices(t *testing.T, s *State, count int, want []int) {
	t.Helper()
	if s.MessageCount != count || !reflect.DeepEqual(indices(s), want) {
		t.Fatalf("count=%d ordinals=%v, want %d %v; items=%+v", s.MessageCount, indices(s), count, want, s.Items)
	}
}
func TestMessageOrdinalsFullBranchAndContentBlocks(t *testing.T) {
	var s State
	entries := []map[string]any{
		{"id": "model", "type": "model_change", "parentId": nil, "modelId": "m"},
		ordinalEntry("u", "model", `{"role":"user","content":[{"type":"text","text":"first"},{"type":"text","text":"second block"},{"type":"image","mimeType":"image/png","data":"a"}]}`),
		ordinalEntry("hidden", "u", `{"role":"custom","display":false,"content":"not displayed"}`),
		{"id": "thinking", "type": "thinking_level_change", "parentId": "hidden", "thinkingLevel": "high"},
		ordinalEntry("a", "thinking", `{"role":"assistant","content":[{"type":"thinking","thinking":"invisible when UI hides thinking"}]}`),
		{"id": "compact", "type": "compaction", "parentId": "a", "summary": "retained originals"},
		{"id": "branch", "type": "branch_summary", "parentId": "compact", "summary": "branch note"},
		ordinalEntry("v", "branch", `{"role":"user","content":"last"}`),
		{"id": "name", "type": "session_info", "parentId": "v", "name": "named"},
		ordinalEntry("sibling", "u", `{"role":"assistant","content":"sibling only"}`),
	}
	ordinalEntries(t, &s, entries, "name")
	requireIndices(t, &s, 5, []int{1, 2, 3, 4, 5})
	if s.Name != "named" {
		t.Fatal("session metadata changed")
	}
	ordinalEntries(t, &s, entries, "sibling")
	requireIndices(t, &s, 2, []int{1, 2})
	ordinalEntries(t, &s, entries, nil)
	requireIndices(t, &s, 0, []int{})
}
func TestMessageOrdinalsToolsNeedNativeResultRecord(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"tool_execution_start","toolCallId":"call","toolName":"edit","args":{"path":"f","oldText":"a","newText":"b"}}`)
	requireIndices(t, &s, 0, []int{0})
	user := `{"role":"user","timestamp":1,"content":"change it"}`
	assistant := `{"role":"assistant","timestamp":2,"content":[{"type":"thinking","thinking":"plan"},{"type":"text","text":"editing"},{"type":"toolCall","id":"call","name":"edit","arguments":{"path":"f","oldText":"a","newText":"b"}}]}`
	apply(&s, `{"type":"message_end","message":`+user+`}`)
	apply(&s, `{"type":"message_end","message":`+assistant+`}`)
	requireIndices(t, &s, 2, []int{1, 2, 2, 2})
	apply(&s, `{"type":"tool_execution_update","toolCallId":"call","partialResult":{"content":[{"type":"text","text":"partial"}],"details":{"diff":"-1 a\n+1 b"}}}`)
	requireIndices(t, &s, 2, []int{1, 2, 2, 2})
	apply(&s, `{"type":"tool_execution_end","toolCallId":"call","isError":true,"result":{"content":[{"type":"text","text":"failed"}],"details":{"diff":"-1 a\n+1 b"}}}`)
	requireIndices(t, &s, 2, []int{1, 2, 2, 2})
	result := `{"role":"toolResult","toolCallId":"call","toolName":"edit","timestamp":3,"isError":true,"content":[{"type":"text","text":"failed"}],"details":{"diff":"-1 a\n+1 b"}}`
	apply(&s, `{"type":"message_end","message":`+result+`}`)
	requireIndices(t, &s, 3, []int{1, 2, 2, 3})
	original := s.Items[3]
	if !original.Error || !original.HasResult || len(original.Details) == 0 || len(original.Arguments) == 0 || original.EstimatedTokens == 0 {
		t.Fatalf("tool data lost: %+v", original)
	}
	apply(&s, `{"type":"tool_execution_update","toolCallId":"call","partialResult":{"content":[{"type":"text","text":"late"}]}}`)
	requireIndices(t, &s, 3, []int{1, 2, 2, 3})
	apply(&s, `{"type":"agent_settled"}`)
	entries := []map[string]any{ordinalEntry("u", nil, user), ordinalEntry("a", "u", assistant), ordinalEntry("r", "a", result)}
	ordinalEntries(t, &s, entries, "r")
	requireIndices(t, &s, 3, []int{1, 2, 2, 3})
	if s.Items[3].ID != original.ID || s.Items[3].EstimatedTokens != original.EstimatedTokens || string(s.Items[3].Details) != string(original.Details) {
		t.Fatal("ordinal projection altered tool identity, payload or estimate")
	}
	ordinalEntries(t, &s, entries, "r")
	requireIndices(t, &s, 3, []int{1, 2, 2, 3})
}
func TestMessageOrdinalsStreamingAndFallback(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"message_start","message":{"role":"assistant","timestamp":1,"content":[]}}`)
	requireIndices(t, &s, 1, []int{})
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"think"}}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"hello"}}`)
	requireIndices(t, &s, 1, []int{1, 1})
	final := `{"role":"assistant","timestamp":1,"content":[{"type":"thinking","thinking":"think"},{"type":"text","text":"hello"}]}`
	liveID := s.Items[0].ID
	// An authoritative refresh can arrive before the matching message_end.
	ordinalEntries(t, &s, []map[string]any{ordinalEntry("a", nil, final)}, "a")
	requireIndices(t, &s, 1, []int{1, 1})
	if s.Items[0].ID != liveID {
		t.Fatal("refresh changed a streaming block ID")
	}
	apply(&s, `{"type":"message_end","message":`+final+`}`)
	apply(&s, `{"type":"agent_settled"}`)
	ordinalEntries(t, &s, []map[string]any{ordinalEntry("a", nil, final)}, "a")
	requireIndices(t, &s, 1, []int{1, 1})
	if err := s.LoadMessages([]byte(`{"messages":[` + final + `,{"role":"custom","display":false,"content":"hidden"},{"role":"custom","content":"visible"},{"role":"bashExecution","command":"pwd","output":"/tmp"},{"role":"branchSummary","summary":"branch"}]}`)); err != nil {
		t.Fatal(err)
	}
	requireIndices(t, &s, 4, []int{1, 1, 2, 3, 4})
	apply(&s, `{"type":"message_end","message":{"role":"custom","display":false,"timestamp":5,"content":"hidden again"}}`)
	requireIndices(t, &s, 4, []int{1, 1, 2, 3, 4})
	if err := s.LoadState([]byte(`{"sessionId":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadState([]byte(`{"sessionId":"new"}`)); err != nil {
		t.Fatal(err)
	}
	requireIndices(t, &s, 0, []int{})
}
