package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func assertFreshProjection(t *testing.T, s *State) {
	t.Helper()
	fresh := *s
	fresh.records = append([]record(nil), s.records...)
	fresh.live = append([]record(nil), s.live...)
	for _, records := range [][]record{fresh.records, fresh.live} {
		for i := range records {
			records[i].projection = nil
		}
	}
	fresh.tools = make(map[string]Item, len(s.tools))
	for id, item := range s.tools {
		item.argumentCharsKnown = false
		fresh.tools[id] = item
	}
	fresh.project()
	if fresh.MessageCount != s.MessageCount || !reflect.DeepEqual(fresh.Items, s.Items) {
		t.Fatalf("cached projection differs from fresh projection\ncached=%+v\nfresh=%+v", s.Items, fresh.Items)
	}
}

func TestProjectionCacheReusesHistoryAndInvalidatesCurrentVersion(t *testing.T) {
	var s State
	if err := s.LoadMessages(json.RawMessage(`{"messages":[{"role":"user","content":"history"},{"role":"assistant","content":[{"type":"toolCall","id":"old","name":"read","arguments":{"path":"a"}}]},{"role":"toolResult","toolCallId":"old","content":[{"type":"text","text":"result"}]}]}`)); err != nil {
		t.Fatal(err)
	}
	old := make([]*recordProjection, len(s.live))
	for i := range s.live {
		old[i] = s.live[i].projection
	}
	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","timestamp":123,"content":[{"type":"thinking","thinking":"first"}]}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":" next"}}`,
		`{"type":"message_update","message":{"role":"assistant","timestamp":123,"content":[{"type":"text","text":"replacement"},{"type":"toolCall","id":"new","name":"bash","arguments":{"command":"echo 😀"}}]}}`,
		`{"type":"tool_execution_start","toolCallId":"new","toolName":"bash","args":{"command":"echo 😀"}}`,
		`{"type":"tool_execution_update","toolCallId":"new","partialResult":{"content":[{"type":"text","text":"partial"}],"details":{"exitCode":0}}}`,
		`{"type":"tool_execution_end","toolCallId":"new","result":{"content":[{"type":"text","text":"final"}],"isError":true}}`,
		`{"type":"message_end","message":{"role":"toolResult","toolCallId":"new","content":[{"type":"text","text":"authoritative"}],"isError":false}}`,
		`{"type":"message_end","message":{"role":"assistant","timestamp":123,"content":[{"type":"text","text":"done"}]}}`,
		`{"type":"agent_settled"}`,
	}
	for _, event := range events {
		apply(&s, event)
		assertFreshProjection(t, &s)
		for i, p := range old {
			if s.live[i].projection != p {
				t.Fatalf("history %d reparsed after %s", i, event)
			}
		}
	}
	if !strings.Contains(texts(&s), "done") || strings.Contains(texts(&s), "replacement") {
		t.Fatal("stale active message projection")
	}
	for _, item := range s.Items {
		if item.ToolID == "new" && (item.Running || item.Error || item.Text != "authoritative") {
			t.Fatalf("stale tool overlay: %+v", item)
		}
	}
}

func TestProjectionCacheDynamicFlagsAndHiddenRecords(t *testing.T) {
	var s State
	if err := s.LoadMessages(json.RawMessage(`{"messages":[{"role":"custom","display":false,"content":"hidden"},{"role":"assistant","content":[{"type":"text","text":"visible"},{"type":"thinking","thinking":"reason"},{"type":"toolCall","id":"t","name":"read","arguments":{"path":"a"}},{"type":"image"}]},{"role":"bashExecution","command":"echo 😀","output":"😀","exitCode":0},{"role":"compactionSummary","summary":"summary"}]}`)); err != nil {
		t.Fatal(err)
	}
	s.Busy = true
	s.active = s.live[1].id
	s.project()
	p := s.live[1].projection
	if s.MessageCount != 3 {
		t.Fatalf("hidden record counted: %d", s.MessageCount)
	}
	for _, item := range s.Items {
		if item.Text == "visible" || item.Thinking == "reason" || item.ToolID == "t" {
			if !item.Running {
				t.Fatalf("missing dynamic running: %+v", item)
			}
		}
	}
	s.Busy = false
	s.active = ""
	s.project()
	if s.live[1].projection != p {
		t.Fatal("dynamic flags unnecessarily invalidated history")
	}
	for _, item := range s.Items {
		if item.Running {
			t.Fatalf("stale running: %+v", item)
		}
	}
	assertFreshProjection(t, &s)
}

func TestProjectionCacheReloadAndBranchRemoval(t *testing.T) {
	var s State
	for _, entries := range []string{
		`{"entries":[{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"old"}},{"type":"message","id":"b","parentId":"a","message":{"role":"assistant","content":"remove"}}],"leafId":"b"}`,
		`{"entries":[{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"new"}}],"leafId":"a"}`,
	} {
		if err := s.LoadEntries(json.RawMessage(entries)); err != nil {
			t.Fatal(err)
		}
		assertFreshProjection(t, &s)
	}
	if s.MessageCount != 1 || !strings.Contains(texts(&s), "new") || strings.Contains(texts(&s), "old") || strings.Contains(texts(&s), "remove") {
		t.Fatalf("stale branch: %s", texts(&s))
	}
}

func TestProjectionCacheReplacesArgumentEstimate(t *testing.T) {
	var s State
	apply(&s, `{"type":"tool_execution_start","toolCallId":"t","toolName":"read","args":{"path":"a"}}`)
	first := s.Items[0].EstimatedTokens
	apply(&s, `{"type":"tool_execution_update","toolCallId":"t","args":{"path":"a much longer path for a replacement argument"},"partialResult":{"content":[{"type":"text","text":"output"}]}}`)
	if s.Items[0].EstimatedTokens <= first {
		t.Fatal("argument estimate was not invalidated")
	}
	assertFreshProjection(t, &s)
	apply(&s, `{"type":"tool_execution_end","toolCallId":"t","result":{"content":[]}}`)
	assertFreshProjection(t, &s)
}

func BenchmarkApplyStreamingHistory(b *testing.B) {
	for _, n := range []int{10, 100} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			messages := make([]map[string]any, 0, n*2)
			payload := strings.Repeat("historical payload 😀 ", 1024)
			for i := 0; i < n; i++ {
				id := fmt.Sprint(i)
				messages = append(messages, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": id, "name": "write", "arguments": map[string]any{"content": payload}}}}, map[string]any{"role": "toolResult", "toolCallId": id, "content": []any{map[string]any{"type": "text", "text": payload}}})
			}
			raw, _ := json.Marshal(map[string]any{"messages": messages})
			var s State
			if err := s.LoadMessages(raw); err != nil {
				b.Fatal(err)
			}
			event := json.RawMessage(`{"type":"message_update","message":{"role":"assistant","timestamp":123,"content":[{"type":"thinking","thinking":"current snapshot"}]}}`)
			s.Apply(event)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s.Apply(event)
			}
		})
	}
}
