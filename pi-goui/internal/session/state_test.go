package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func apply(s *State, v string) bool { return s.Apply(json.RawMessage(v)) }
func texts(s *State) string {
	var b strings.Builder
	for _, i := range s.Items {
		b.WriteString(i.Text)
		b.WriteString(i.Thinking)
		b.WriteByte('\n')
	}
	return b.String()
}
func TestFullBranchKeepsCompactedMessages(t *testing.T) {
	var s State
	raw := `{"entries":[{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"old request","timestamp":1}},{"type":"message","id":"b","parentId":"a","message":{"role":"assistant","content":[{"type":"text","text":"old answer"}],"timestamp":2}},{"type":"message","id":"sibling","parentId":"a","message":{"role":"assistant","content":"WRONG BRANCH"}},{"type":"compaction","id":"c","parentId":"b","summary":"short summary"},{"type":"message","id":"d","parentId":"c","message":{"role":"user","content":"new request","timestamp":3}}],"leafId":"d"}`
	if err := s.LoadEntries([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	out := texts(&s)
	for _, want := range []string{"old request", "old answer", "short summary", "new request"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lost %s: %s", want, out)
		}
	}
	if strings.Contains(out, "WRONG") {
		t.Fatal("sibling leaked")
	}
	if err := s.LoadMessages([]byte(`{"messages":[{"role":"compactionSummary","summary":"fallback"}]}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(&s), "old answer") {
		t.Fatal("fallback erased history")
	}
	if err := s.LoadEntries([]byte(`{"entries":[],"leafId":null}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 0 {
		t.Fatalf("null leaf not empty: %+v", s.Items)
	}
}
func TestStreamingLifecycleAndAuthoritativeRefresh(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"message_start","message":{"role":"assistant","timestamp":1,"content":[]}}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"思考"}}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"hello "}}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"世界"}}`)
	if !strings.Contains(texts(&s), "hello 世界") || !strings.Contains(texts(&s), "思考") {
		t.Fatal(texts(&s))
	}
	id := s.Items[1].ID
	apply(&s, `{"type":"agent_end"}`)
	if !s.Busy {
		t.Fatal("agent_end ended run")
	}
	s.LoadState([]byte(`{"sessionId":"native","isStreaming":false}`))
	if !s.Busy {
		t.Fatal("racing state snapshot ended run")
	}
	apply(&s, `{"type":"message_end","message":{"role":"assistant","timestamp":1,"content":[{"type":"thinking","thinking":"思考"},{"type":"text","text":"authoritative"}]}}`)
	if s.Items[1].ID != id || strings.Contains(texts(&s), "hello") {
		t.Fatalf("not replaced stably: %+v", s.Items)
	}
	if err := s.LoadEntries([]byte(`{"entries":[{"type":"message","id":"persisted","parentId":null,"message":{"role":"assistant","timestamp":1,"content":[{"type":"thinking","thinking":"思考"},{"type":"text","text":"authoritative"}]}}],"leafId":"persisted"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 2 || s.Items[1].ID != id {
		t.Fatalf("refresh duplicated/renamed live message: %+v", s.Items)
	}
	if !apply(&s, `{"type":"agent_settled"}`) || s.Busy {
		t.Fatal("settled lifecycle wrong")
	}
}
func TestToolsQueueAndState(t *testing.T) {
	var s State
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"message_end","message":{"role":"assistant","timestamp":1,"content":[{"type":"toolCall","id":"call1","name":"bash","arguments":{"command":"pwd"}}]}}`)
	apply(&s, `{"type":"tool_execution_start","toolCallId":"call1","toolName":"bash","args":{"command":"pwd"}}`)
	apply(&s, `{"type":"tool_execution_update","toolCallId":"call1","partialResult":{"content":[{"type":"text","text":"partial"}]}}`)
	if len(s.Items) != 1 || !s.Items[0].Running || s.Items[0].Text != "partial" {
		t.Fatalf("bad partial: %+v", s.Items)
	}
	apply(&s, `{"type":"tool_execution_end","toolCallId":"call1","isError":true,"result":{"content":[{"type":"text","text":"final"}]}}`)
	apply(&s, `{"type":"message_end","message":{"role":"toolResult","toolCallId":"call1","toolName":"bash","isError":true,"timestamp":2,"content":[{"type":"text","text":"final"}]}}`)
	if len(s.Items) != 1 || s.Items[0].Running || !s.Items[0].Error || string(s.Items[0].Arguments) != `{"command":"pwd"}` {
		t.Fatalf("duplicate/bad tool: %+v", s.Items)
	}
	apply(&s, `{"type":"queue_update","steering":["steer"],"followUp":["later"]}`)
	if len(s.Steering) != 1 || len(s.FollowUp) != 1 {
		t.Fatal("queue missing")
	}
	if err := s.LoadStats([]byte(`{"tokens":{"input":20,"output":5,"cacheRead":2,"cacheWrite":3},"cost":0,"contextUsage":{"tokens":null,"percent":null,"contextWindow":1000}}`)); err != nil {
		t.Fatal(err)
	}
	if s.Stats.Total != 30 || !s.Stats.HasCost || s.Stats.ContextTokens != nil {
		t.Fatalf("stats: %+v", s.Stats)
	}
}
func TestSameTimestampMessagesRemainDistinct(t *testing.T) {
	var s State
	apply(&s, `{"type":"message_end","message":{"role":"user","timestamp":1,"content":"one"}}`)
	apply(&s, `{"type":"message_end","message":{"role":"user","timestamp":1,"content":"two"}}`)
	if len(s.Items) != 2 {
		t.Fatalf("timestamp collision lost message: %+v", s.Items)
	}
	if err := s.LoadEntries([]byte(`{"entries":[{"type":"message","id":"a","parentId":null,"message":{"role":"user","timestamp":1,"content":"one"}},{"type":"message","id":"b","parentId":"a","message":{"role":"user","timestamp":1,"content":"two"}}],"leafId":"b"}`)); err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 2 || s.Items[0].ID == s.Items[1].ID {
		t.Fatalf("refresh collided: %+v", s.Items)
	}
}

func TestRefreshDoesNotEraseLiveTail(t *testing.T) {
	var s State
	base := []byte(`{"entries":[{"type":"message","id":"a","parentId":null,"message":{"role":"user","timestamp":1,"content":"persisted"}}],"leafId":"a"}`)
	if err := s.LoadEntries(base); err != nil {
		t.Fatal(err)
	}
	apply(&s, `{"type":"agent_start"}`)
	apply(&s, `{"type":"message_start","message":{"role":"assistant","timestamp":2,"content":[]}}`)
	apply(&s, `{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"live tail"}}`)
	if err := s.LoadEntries(base); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(&s), "live tail") || !s.Busy {
		t.Fatalf("refresh regressed stream: %+v", s)
	}
}

func TestInvalidBranchRejected(t *testing.T) {
	for _, raw := range []string{`{"entries":[],"leafId":"missing"}`, `{"entries":[{"id":"a","parentId":"a"}],"leafId":"a"}`} {
		var s State
		if s.LoadEntries([]byte(raw)) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestSessionFilesReadOnlyAndIncompleteTail(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	dir := filepath.Join(root, "sessions", "encoded")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "session.jsonl")
	header, _ := json.Marshal(map[string]any{"type": "session", "id": "native-id", "cwd": cwd})
	data := string(header) + "\n" + `{"type":"message","id":"a","parentId":null,"message":{"role":"user","content":"你好","timestamp":1}}` + "\n" + `{"type":"session_info","id":"b","parentId":"a","name":"test session"}` + "\n" + `{"type":"message"`
	os.WriteFile(path, []byte(data), 0600)
	raw, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s State
	if err = s.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts(&s), "你好") {
		t.Fatal("missing utf8")
	}
	list, err := List(root, cwd)
	if err != nil || len(list) != 1 || list[0].Name != "test session" || list[0].FirstMessage != "你好" {
		t.Fatalf("list %+v %v", list, err)
	}
	other, err := List(root, t.TempDir())
	if err != nil || len(other) != 0 {
		t.Fatal("cwd filter failed")
	}
	after, _ := os.ReadFile(path)
	if string(after) != data {
		t.Fatal("history modified")
	}
}
