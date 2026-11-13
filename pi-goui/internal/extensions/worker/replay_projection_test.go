package worker

import "testing"

func TestWorkerReplayInjectsProjectionAfterReset(t *testing.T) {
	var replay Replay
	plan := `{"type":"message_end","message":{"role":"toolResult","toolName":"update_plan","toolCallId":"p","details":{"plan":[{"id":"a","goal":"Inspect","status":"completed"}]}}}`
	hidden := `{"type":"message_end","message":{"role":"custom","customType":"web-search-content-ready","content":[{"type":"text","text":"background"}]}}`
	for i := 0; i < 2; i++ {
		if i > 0 {
			replay.Apply(ReplayRead{Batch: Batch{Reset: true}})
			if len(replay.View().Plan) != 0 {
				t.Fatal("reset retained previous plan")
			}
		}
		replay.Apply(ReplayRead{Batch: replayBatch(plan, hidden)})
		view := replay.View()
		if len(view.Plan) != 1 || view.Plan[0].Status != "completed" {
			t.Fatalf("round %d: projection missing: %+v", i, view.Plan)
		}
		if view.MessageCount != 2 || len(view.Items) != 1 || view.Items[0].ToolName != "update_plan" {
			t.Fatalf("round %d: visibility or ordinal mismatch: %+v", i, view)
		}
	}
}
