package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSamePrefixDetectsStreamVersionChange(t *testing.T) {
	message := message{Role: "assistant", StopReason: "stop", stream: []content{{Type: "text", Text: "before"}}}
	reply := record{id: "reply", key: keyOf(message), msg: message}
	verified := ForkCheckpoint{records: []record{reply}, leaf: "boundary", pathIDs: map[string]bool{"reply": true}}
	plan := ForkPreview{NextUserID: "boundary", ReplyCompleted: true, history: []record{reply}, prefixIDs: []string{"reply"}, branchIDs: map[string]bool{"boundary": true}}
	if !verified.Matches(plan) || !verified.SamePrefix(verified, plan) {
		t.Fatal("unchanged immutable stream version failed verification")
	}
	current := verified
	current.records = append([]record(nil), verified.records...)
	current.records[0].msg.stream = []content{{Type: "text", Text: "after"}}
	// Delta updates intentionally do not recompute the canonical record key.
	if current.SamePrefix(verified, plan) || current.Matches(plan) {
		t.Fatal("changed streamed content accepted via its old key")
	}
	current.records[0].msg.stream = nil
	current.records[0].msg.Content = json.RawMessage(`[{"type":"text","text":"after"}]`)
	if current.SamePrefix(verified, plan) {
		t.Fatal("replacement snapshot accepted as the old stream version")
	}
}

func streamTestDelta(s *State, kind string, index int, value any) {
	event := map[string]any{"type": kind, "contentIndex": index}
	switch kind {
	case "text_end", "thinking_end":
		event["content"] = value
	case "toolcall_start":
		event["id"], event["toolName"] = "tool", "write"
	case "toolcall_end":
		event["toolCall"] = value
	default:
		event["delta"] = value
	}
	raw, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": event})
	s.Apply(raw)
}

func TestStreamedBlocksWireProjectionAndVersionIsolation(t *testing.T) {
	var state State
	apply(&state, `{"type":"message_start","message":{"role":"assistant","timestamp":1,"content":[]}}`)
	steps := []struct {
		kind  string
		index int
		value any
	}{
		{"text_start", 0, ""},
		{"text_delta", 0, "hello \"世界\"\n"},
		{"text_delta", 0, "👩🏽‍💻"},
		{"thinking_start", 1, ""},
		{"thinking_delta", 1, "think"},
		{"toolcall_start", 2, ""},
		{"toolcall_delta", 2, `{"path":"a.txt","content":"first\n中`},
		{"toolcall_delta", 2, `文\ud83d`},
		{"toolcall_delta", 2, `\ude00`},
		{"toolcall_end", 2, map[string]any{"type": "toolCall", "id": "tool", "name": "write", "arguments": map[string]any{"path": "a.txt", "content": "final", "unknown": true}}},
		{"thinking_end", 1, "authoritative thinking"},
		{"text_end", 0, "authoritative text"},
		// Replacing an earlier block must not mutate a previously published
		// slice, even when its backing array has spare capacity.
		{"text_delta", 0, " suffix"},
	}
	for _, step := range steps {
		t.Run(step.kind, func(t *testing.T) {
			previous := state.live[0].msg
			before, err := json.Marshal(previous)
			if err != nil {
				t.Fatal(err)
			}
			oldItems := append([]Item(nil), state.Items...)
			beforeItems, _ := json.Marshal(oldItems)
			streamTestDelta(&state, step.kind, step.index, step.value)
			after, _ := json.Marshal(previous)
			if !bytes.Equal(before, after) {
				t.Fatal("delta changed an earlier message version")
			}
			afterItems, _ := json.Marshal(oldItems)
			if !bytes.Equal(beforeItems, afterItems) {
				t.Fatal("delta changed published items")
			}
			current := state.live[0].msg
			if current.stream == nil || current.Content != nil {
				t.Fatal("delta did not replace raw content with authoritative blocks")
			}
			raw, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			var decoded message
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.stream != nil || keyOf(current) != keyOf(decoded) {
				t.Fatal("streamed and wire content identities differ")
			}
			wire := record{id: state.live[0].id, msg: decoded}
			if !reflect.DeepEqual(wire.project(), state.live[0].projection) {
				t.Fatal("streamed and wire projections differ")
			}
		})
	}
	if !strings.Contains(texts(&state), "authoritative text suffix") || !strings.Contains(texts(&state), "authoritative thinking") {
		t.Fatal("terminal block content did not replace deltas")
	}
}

func TestStreamedContentSnapshotAndTerminalReplacement(t *testing.T) {
	for _, terminal := range []string{"message_update", "message_end", "done", "error"} {
		t.Run(terminal, func(t *testing.T) {
			var state State
			streamTestDelta(&state, "text_delta", 0, "old delta")
			msg := json.RawMessage(`{"role":"assistant","timestamp":1,"stopReason":"stop","content":[{"type":"text","text":"replacement","signature":{"future":true}}]}`)
			var raw []byte
			if terminal == "done" || terminal == "error" {
				field := "message"
				if terminal == "error" {
					field = "error"
				}
				raw, _ = json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": terminal, field: msg}})
			} else {
				raw, _ = json.Marshal(map[string]any{"type": terminal, "message": msg})
			}
			state.Apply(raw)
			if len(state.live) != 1 || state.live[0].msg.stream != nil || strings.Contains(texts(&state), "old delta") || !strings.Contains(texts(&state), "replacement") {
				t.Fatal("authoritative snapshot did not replace in-flight blocks")
			}
			encoded, _ := json.Marshal(state.live[0].msg)
			if !bytes.Contains(encoded, []byte(`"signature":{"future":true}`)) {
				t.Fatal("snapshot raw content lost an unknown field")
			}
			if terminal != "message_update" && state.active != "" {
				t.Fatal("terminal event left assistant active")
			}
		})
	}
}

func TestStreamedBlocksPrepareAndCheckpointRemainImmutable(t *testing.T) {
	var state State
	if err := state.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	streamTestDelta(&state, "text_delta", 0, "captured")
	streamTestDelta(&state, "thinking_delta", 1, "thought")
	streamTestDelta(&state, "toolcall_start", 2, "")
	streamTestDelta(&state, "toolcall_delta", 2, `{"content":"old`)
	checkpoint := state.ForkCheckpoint()
	preparation := state.SnapshotPreparation()
	captured := checkpoint.records[len(checkpoint.records)-1].msg
	before, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	beforeKey := keyOf(captured)
	var candidate *PreparedSnapshot
	var prepareErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			candidate, prepareErr = preparation.Prepare(nil, json.RawMessage(preparedHistory), nil)
			if prepareErr != nil {
				return
			}
			// Hashing reads every captured block while the owner replaces its
			// active content. The race detector exercises this shared boundary.
			if keyOf(captured) != beforeKey {
				prepareErr = fmt.Errorf("checkpoint changed during preparation")
				return
			}
		}
	}()
	for i := 0; i < 50; i++ {
		streamTestDelta(&state, "text_delta", 0, " later")
		streamTestDelta(&state, "thinking_delta", 1, " later")
		streamTestDelta(&state, "toolcall_delta", 2, " later")
	}
	wg.Wait()
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	after, _ := json.Marshal(captured)
	if !bytes.Equal(before, after) {
		t.Fatal("checkpoint retained mutable content")
	}
	var target State
	target.ApplySnapshot(candidate)
	if strings.Contains(texts(&target), "later") || !strings.Contains(texts(&target), "captured") {
		t.Fatal("background preparation observed a later content version")
	}
	// Two owners may adopt the same prepared result and continue independently.
	streamTestDelta(&target, "text_delta", 0, " target")
	var other State
	other.ApplySnapshot(candidate)
	if strings.Contains(texts(&other), "target") || strings.Contains(texts(&other), "later") {
		t.Fatal("adoption made prepared blocks mutable")
	}
}

func TestStreamedContentReconcilesWithNativeHistory(t *testing.T) {
	var state State
	if err := state.LoadEntries(json.RawMessage(preparedHistory)); err != nil {
		t.Fatal(err)
	}
	streamTestDelta(&state, "text_delta", 0, "first")
	streamTestDelta(&state, "text_delta", 0, " second")
	alias := state.live[0].id
	messageBytes, err := json.Marshal(state.live[0].msg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := json.RawMessage(`{"entries":[` +
		`{"id":"u","type":"message","parentId":null,"message":{"role":"user","content":"hello"}},` +
		`{"id":"a","type":"message","parentId":"u","message":{"role":"assistant","content":"old","stopReason":"stop"}},` +
		`{"id":"next","type":"message","parentId":"a","message":` + string(messageBytes) + `}],"leafId":"next"}`)
	if err := state.LoadEntries(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(state.live) != 0 || len(state.records) != 3 || state.records[2].id != alias || state.records[2].entryID != "next" {
		t.Fatal("native reconciliation duplicated or renamed the streamed record")
	}
	if strings.Count(texts(&state), "first second") != 1 {
		t.Fatal("reconciled transcript lost or duplicated streamed content")
	}
}

func BenchmarkApplyTextDelta(b *testing.B) {
	for _, size := range []int{64 << 10, 256 << 10, 1 << 20} {
		for _, roundtrip := range []bool{false, true} {
			mode := "stream"
			if roundtrip {
				mode = "json-roundtrip-reference"
			}
			b.Run(fmt.Sprintf("%dKiB/%s", size>>10, mode), func(b *testing.B) {
				var state State
				streamTestDelta(&state, "text_delta", 0, strings.Repeat("x", size))
				original := state.live[0]
				if roundtrip {
					original.msg.Content, _ = json.Marshal(original.msg.stream)
					original.msg.stream = nil
				}
				delta := json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl"}}`)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					state.live[0] = original
					if roundtrip {
						benchmarkJSONRoundTripTextDelta(&state, delta)
					} else {
						state.Apply(delta)
					}
				}
			})
		}
	}
}

// Isolate the previous text-delta algorithm for allocation/time comparisons.
// This deliberately decodes and serializes the complete growing content before
// projection, rather than reusing the immutable in-flight representation.
func benchmarkJSONRoundTripTextDelta(state *State, raw json.RawMessage) {
	var event struct {
		AssistantMessageEvent struct {
			ContentIndex int
			Delta        string
		}
	}
	_ = json.Unmarshal(raw, &event)
	r := &state.live[0]
	content := blocks(r.msg.Content)
	content[event.AssistantMessageEvent.ContentIndex].Text += event.AssistantMessageEvent.Delta
	r.msg.Content, _ = json.Marshal(content)
	r.projection = nil
	state.project()
}
