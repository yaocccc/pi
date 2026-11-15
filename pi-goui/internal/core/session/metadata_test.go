package session

import (
	"encoding/json"
	"testing"
)

func TestSnapshotMetadataKeepsEventsAfterQueryCheckpoint(t *testing.T) {
	var state State
	if err := state.LoadState(json.RawMessage(`{"sessionId":"one","thinkingLevel":"low"}`)); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.MetadataRevision()
	// The query samples the middle of a start/end pair. Comparing values at
	// query start and the history fence would miss this false -> true -> false.
	for _, event := range []string{
		`{"type":"agent_start"}`,
		`{"type":"compaction_start"}`,
		`{"type":"compaction_end"}`,
		`{"type":"agent_settled"}`,
		`{"type":"thinking_level_changed","level":"high"}`,
		`{"type":"session_info_changed","name":"new name"}`,
		`{"type":"queue_update","steering":["new queue"],"followUp":[]}`,
	} {
		state.Apply(json.RawMessage(event))
	}
	preparation := state.SnapshotPreparation()
	// Mutating the live metadata after capture cannot mutate the preparation's
	// version map or field values (the UI also rejects the stale revision).
	state.Apply(json.RawMessage(`{"type":"thinking_level_changed","level":"off"}`))
	if preparation.base.metadataChanges["thinkingLevel"] == state.metadataChanges["thinkingLevel"] {
		t.Fatal("preparation retained the mutable metadata version map")
	}
	prepared, err := preparation.PrepareAfter(json.RawMessage(`{"sessionId":"one","sessionName":"old name","thinkingLevel":"low","isStreaming":true,"isCompacting":true,"steering":["old queue"],"followUp":["old followup"],"model":{"id":"fresh-model"}}`), json.RawMessage(`{"entries":[]}`), nil, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	state.ApplySnapshot(prepared)
	if state.Busy || state.Compacting || state.Thinking != "high" || state.Name != "new name" || state.Model.ID != "fresh-model" {
		t.Fatalf("metadata regressed: busy=%v compacting=%v thinking=%s name=%s model=%s", state.Busy, state.Compacting, state.Thinking, state.Name, state.Model.ID)
	}
	if len(state.Steering) != 1 || state.Steering[0] != "new queue" || len(state.FollowUp) != 0 {
		t.Fatalf("queue metadata regressed: %v / %v", state.Steering, state.FollowUp)
	}
	// Without intervening events, a later authoritative state may clear flags
	// and change metadata normally. Compaction must not become sticky.
	state.Compacting = true
	prepared, err = state.SnapshotPreparation().PrepareAfter(json.RawMessage(`{"sessionId":"one","isCompacting":false,"thinkingLevel":"medium"}`), nil, nil, state.MetadataRevision())
	if err != nil {
		t.Fatal(err)
	}
	state.ApplySnapshot(prepared)
	if state.Compacting || state.Thinking != "medium" {
		t.Fatal("a later authoritative state could not update metadata")
	}
}
