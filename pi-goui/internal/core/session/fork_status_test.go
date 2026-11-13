package session

import (
	"encoding/json"
	"strings"
	"testing"
)

func forkStatusSnapshot(t *testing.T, middle ...map[string]any) json.RawMessage {
	t.Helper()
	messages := []map[string]any{{"role": "user", "content": "selected"}}
	messages = append(messages, middle...)
	messages = append(messages, map[string]any{"role": "user", "content": "later"})
	entries := []map[string]any{}
	var parent any
	for i, message := range messages {
		id := string(rune('a' + i))
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	raw, err := json.Marshal(map[string]any{"entries": entries, "leafId": parent})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestForkReplyCompletion(t *testing.T) {
	assistant := func(reason string) map[string]any {
		return map[string]any{"role": "assistant", "content": "answer", "stopReason": reason}
	}
	tool := map[string]any{"role": "assistant", "content": []map[string]any{{"type": "toolCall", "id": "tool", "name": "read", "arguments": map[string]any{}}}, "stopReason": "toolUse"}
	result := map[string]any{"role": "toolResult", "toolCallId": "tool", "content": "done"}
	for _, tc := range []struct {
		name     string
		messages []map[string]any
		complete bool
	}{
		{"no-reply", nil, false},
		{"stop", []map[string]any{assistant("stop")}, true},
		{"length", []map[string]any{assistant("length")}, true},
		{"missing-stop", []map[string]any{assistant("")}, false},
		{"error", []map[string]any{assistant("error")}, false},
		{"aborted", []map[string]any{assistant("aborted")}, false},
		{"toolUse", []map[string]any{tool}, false},
		{"tool-result-only", []map[string]any{tool, result}, false},
		{"tool-unresolved", []map[string]any{tool, assistant("stop")}, false},
		{"tool-finished", []map[string]any{tool, result, assistant("stop")}, true},
		{"late-error", []map[string]any{assistant("stop"), assistant("error")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := forkStatusSnapshot(t, tc.messages...)
			plan, err := PreviewFork("a", raw)
			if err != nil || plan.ReplyCompleted != tc.complete || plan.NextUserID == "" {
				t.Fatalf("completion=%v, next=%q, err=%v", plan.ReplyCompleted, plan.NextUserID, err)
			}
			var state State
			if err := state.LoadEntries(raw); err != nil {
				t.Fatal(err)
			}
			if state.ForkCheckpoint().Matches(plan) != tc.complete {
				t.Fatal("checkpoint completion disagrees with authoritative snapshot")
			}
		})
	}
}

func TestForkCheckpointPreservesMetadataAndEnrichedTools(t *testing.T) {
	_, raw := forkSource(t)
	raw = json.RawMessage(strings.Replace(string(raw), `"stopReason":"error"`, `"stopReason":"stop"`, 1))
	plan, err := PreviewFork("u1", raw)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := state.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	if !state.ForkCheckpoint().Matches(plan) {
		t.Fatal("compaction/custom metadata broke matching history")
	}
	// Message contents remain identical, but a copied native ancestor was removed.
	changed := json.RawMessage(strings.Replace(string(raw), `"parentId":"label"`, `"parentId":"a1"`, 1))
	if err := state.LoadEntries(changed); err != nil {
		t.Fatal(err)
	}
	if state.ForkCheckpoint().Matches(plan) {
		t.Fatal("removed copied ancestor was accepted")
	}

	raw = forkStatusSnapshot(t,
		map[string]any{"role": "assistant", "content": []map[string]any{{"type": "toolCall", "id": "t", "name": "read"}}, "stopReason": "toolUse"},
		map[string]any{"role": "toolResult", "toolCallId": "t", "content": "result"},
		map[string]any{"role": "assistant", "content": "finished", "stopReason": "stop"})
	plan, err = PreviewFork("a", raw)
	if err != nil {
		t.Fatal(err)
	}
	state = State{}
	if err := state.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	state.records[2].msg.Details = json.RawMessage(`{"ui":"enriched"}`)
	if !state.ForkCheckpoint().Matches(plan) {
		t.Fatal("UI-only enrichment invalidated persisted tool history")
	}
}

func TestForkCheckpointAcceptsLiveBoundaryAndRemainsImmutable(t *testing.T) {
	raw := forkStatusSnapshot(t, map[string]any{"role": "assistant", "content": "answer", "stopReason": "stop"})
	plan, err := PreviewFork("a", raw)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	prior, _ := json.Marshal(map[string]any{"entries": data.Entries[:2], "leafId": "b"})
	var state State
	if err := state.LoadEntries(prior); err != nil {
		t.Fatal(err)
	}
	state.Apply(json.RawMessage(`{"type":"message_end","message":{"role":"user","content":"later"}}`))
	checkpoint := state.ForkCheckpoint()
	if !checkpoint.Matches(plan) {
		t.Fatal("live next-user boundary was not recognized")
	}
	state.Apply(json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":100}}`))
	state.Apply(json.RawMessage(`{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"later delta"}],"timestamp":100}}`))
	if !checkpoint.Matches(plan) || !state.ForkCheckpoint().Matches(plan) {
		t.Fatal("later stream invalidated the completed prefix")
	}
	// A live partial tool result inserted into copied history is not complete.
	broken := checkpoint
	broken.records = append([]record(nil), checkpoint.records...)
	broken.records[1].msg.partial = true
	if broken.Matches(plan) {
		t.Fatal("partial copied history accepted")
	}
	broken = checkpoint
	broken.records = append([]record(nil), checkpoint.records...)
	broken.records[1].msg.Content = json.RawMessage(`"changed"`)
	if broken.Matches(plan) || !checkpoint.Matches(plan) {
		t.Fatal("history comparison lost immutability")
	}
}
