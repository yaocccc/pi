package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/extensions/sessionview"
)

func TestApplyBatchMatchesEveryEvent(t *testing.T) {
	var events []json.RawMessage
	for _, raw := range []string{
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":3}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"hello "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"世界"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","contentIndex":1,"delta":"consider"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"thinking_end","contentIndex":1,"content":"considered"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_start","contentIndex":2,"id":"read-1","toolName":"read"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_delta","contentIndex":2,"delta":"{\"path\":\"a.go\"}"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","contentIndex":2,"toolCall":{"type":"toolCall","id":"read-1","name":"read","arguments":{"path":"a.go"}}}}`,
		`{"type":"tool_execution_start","toolCallId":"read-1","toolName":"read","args":{"path":"a.go"}}`,
		`{"type":"tool_execution_update","toolCallId":"read-1","partialResult":{"content":[{"type":"text","text":"part"}]}}`,
		`{"type":"tool_execution_update","toolCallId":"nested","parentToolCallId":"read-1","partialResult":{"content":[{"type":"text","text":"hidden"}]}}`,
		`{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"snapshot"},{"type":"toolCall","id":"read-1","name":"read","arguments":{"path":"a.go"}}],"timestamp":3}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":" tail"}}`,
		`{"type":"tool_execution_end","toolCallId":"read-1","result":{"content":[{"type":"text","text":"complete"}]},"isError":false}`,
		`{"type":"tool_execution_update","toolCallId":"read-1","partialResult":{"content":[{"type":"text","text":"stale"}]}}`,
		`{"type":"tool_execution_end","toolCallId":"plan","toolName":"update_plan","result":{"details":{"plan":[{"id":"one","goal":"keep plan","status":"pending"}]}}}`,
		`{"type":"entry_appended"}`,
		`{"type":"future_event","value":1}`,
		`invalid`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"snapshot tail"},{"type":"toolCall","id":"read-1","name":"read","arguments":{"path":"a.go"}}],"timestamp":3}}`,
		`{"type":"agent_end"}`,
		`{"type":"agent_settled"}`,
		`{"type":"queue_update","steering":["after refresh"],"followUp":["still applied"]}`,
	} {
		events = append(events, json.RawMessage(raw))
	}
	for _, size := range []int{1, 2, 3, 8, len(events)} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var single, batch State
			single.SetExtensionProjection(&sessionview.State{})
			batch.SetExtensionProjection(&sessionview.State{})
			for start := 0; start < len(events); start += size {
				end := min(start+size, len(events))
				wantRefresh := false
				for _, event := range events[start:end] {
					wantRefresh = single.Apply(event) || wantRefresh
				}
				if got := batch.ApplyBatch(events[start:end]); got != wantRefresh {
					t.Fatalf("batch %d: refresh=%v, want %v", start, got, wantRefresh)
				}
				if !reflect.DeepEqual(single, batch) {
					t.Fatalf("batch %d changed final state compared with ordered individual events", start)
				}
			}
			before := batch.Detached()
			if batch.ApplyBatch(nil) || !reflect.DeepEqual(before, batch.Detached()) {
				t.Fatal("empty batch changed state")
			}
		})
	}
}

func TestApplyBatchPreservesPublishedSnapshots(t *testing.T) {
	var state State
	state.Apply(json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[{"type":"text","text":"prefix"}],"timestamp":1}}`))
	delta := json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"!"}}`)
	state.Apply(delta)
	before := state.Detached()
	items := state.Items
	state.ApplyBatch([]json.RawMessage{delta, delta})
	if items[0].Text != "prefix!" || before.Items[0].Text != "prefix!" || before.live[0].msg.contentBlocks()[0].Text != "prefix!" {
		t.Fatal("batch mutated previously published state")
	}
	if state.Items[0].Text != "prefix!!!" || state.deferProjection || state.projectionPending {
		t.Fatal("batch did not publish or left projection deferred")
	}
	state.Apply(delta)
	if state.Items[0].Text != "prefix!!!!" {
		t.Fatal("individual event after batch was not projected")
	}
}

func BenchmarkApplyEventBatch(b *testing.B) {
	for _, history := range []int{100, 1000} {
		var messages []string
		for i := 0; i < history; i++ {
			messages = append(messages, fmt.Sprintf(`{"role":"user","content":"history %d","timestamp":%d}`, i, i))
		}
		var base State
		if err := base.LoadMessages(json.RawMessage(`{"messages":[` + strings.Join(messages, ",") + `]}`)); err != nil {
			b.Fatal(err)
		}
		base.Apply(json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":-1}}`))
		events := make([]json.RawMessage, 64)
		for i := range events {
			events[i] = json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"token "}}`)
		}
		for _, batched := range []bool{false, true} {
			b.Run(fmt.Sprintf("history=%d/batched=%v", history, batched), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					state := base.Detached()
					b.StartTimer()
					if batched {
						state.ApplyBatch(events)
					} else {
						for _, event := range events {
							state.Apply(event)
						}
					}
				}
			})
		}
	}
}
