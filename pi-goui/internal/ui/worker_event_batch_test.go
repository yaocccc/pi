package ui

import (
	"encoding/json"
	"reflect"
	"testing"

	"pi-goui/internal/session"
)

func TestWorkerEventBatchProjectionEquivalence(t *testing.T) {
	var records []json.RawMessage
	add := func(s string) { records = append(records, json.RawMessage(s)) }
	add(`{"type":"agent_start"}`)
	add(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":1}}`)
	for i := 0; i < 100; i++ {
		add(`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,"delta":"思考 👩‍💻\n"}}`)
	}
	for i := 0; i < 100; i++ {
		add(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"中文 🧪\n"}}`)
	}
	add(`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":2,"id":"w","toolName":"write"}}`)
	for _, delta := range []string{`{"path":`, `"x.go",`, `"content":"text"}`} {
		raw, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "toolcall_delta", "contentIndex": 2, "delta": delta}})
		records = append(records, raw)
	}
	add(`{"type":"tool_execution_end","toolCallId":"w","toolName":"write","result":{"content":[{"type":"text","text":"written"}]}}`)
	add(`{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"authoritative"}],"timestamp":1}}`)
	add(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"authoritative"}],"timestamp":1}}`)
	add(`{"type":"agent_settled"}`)
	merged := workerEventBatch(records)
	if len(merged) >= len(records)/2 {
		t.Fatal("deltas were not combined")
	}
	var a, b session.State
	for _, r := range records {
		a.Apply(r)
	}
	for _, r := range merged {
		b.Apply(r)
	}
	if !reflect.DeepEqual(a.Items, b.Items) || a.MessageCount != b.MessageCount || a.Busy != b.Busy {
		t.Fatal("batched replay changed state")
	}
	// Verify intermediate text, before the authoritative snapshot overwrites it.
	a, b = session.State{}, session.State{}
	for _, r := range records[:201] {
		a.Apply(r)
	}
	for _, r := range workerEventBatch(records[:201]) {
		b.Apply(r)
	}
	if !reflect.DeepEqual(a.Items, b.Items) {
		t.Fatal("Unicode/thinking deltas changed")
	}
}
