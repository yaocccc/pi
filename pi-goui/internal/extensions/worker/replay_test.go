package worker

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func replayBatch(records ...string) Batch {
	var batch Batch
	for _, record := range records {
		batch.Records = append(batch.Records, json.RawMessage(record))
	}
	return batch
}

func TestWorkerReplayOwnsInheritedBoundaryAndReset(t *testing.T) {
	var replay Replay
	header := `{"type":"worker_transcript_header","cwd":"/project","entries":[{"type":"message","id":"u","message":{"role":"user","content":"inherited","timestamp":1}},{"type":"message","id":"t","parentId":"u","message":{"role":"toolResult","toolCallId":"old","toolName":"read","content":[{"type":"text","text":"old output"}],"timestamp":2}}]}`
	replay.Apply(ReplayRead{Batch: replayBatch(header)})
	view := replay.View()
	if len(view.Items) != 0 || view.Cwd != "/project" || view.MessageCount != 2 || len(replay.state.Items) != 2 {
		t.Fatalf("header boundary lost: view=%+v state=%+v", view, replay.state.Items)
	}
	replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"tool_execution_start","toolCallId":"new","toolName":"read","args":{"path":"new.go"}}`)})
	if items := replay.View().Items; len(items) != 1 || items[0].ToolID != "new" || items[0].MessageIndex != 0 {
		t.Fatalf("unattributed live tool hidden: %+v", items)
	}
	replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","id":"new","name":"read","arguments":{"path":"new.go"}}],"timestamp":3}}`)})
	view = replay.View()
	var calls int
	for _, item := range view.Items {
		if item.ToolID == "new" {
			calls++
			if item.MessageIndex <= 2 {
				t.Fatal("tool was not attributed to the task")
			}
		}
	}
	if calls != 1 || len(replay.state.Items)-len(view.Items) != 2 {
		t.Fatal("attribution duplicated a tool or destroyed inherited projection context")
	}
	replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"worker_transcript_end","status":"completed","complete":true}`)})
	reset := replayBatch(`{"type":"worker_transcript_header","cwd":"/replacement","entries":[]}`)
	reset.Reset = true
	replay.Apply(ReplayRead{Batch: reset})
	if replay.Stopped || replay.View().MessageCount != 0 || len(replay.View().Items) != 0 || replay.inheritedMessages != 0 || replay.View().Cwd != "/replacement" {
		t.Fatal("replacement retained old projection, boundary or terminal state")
	}
	replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_end","message":{"role":"user","content":"new task","timestamp":4}}`)})
	if items := replay.View().Items; len(items) != 1 || items[0].Text != "new task" {
		t.Fatal("replacement suppressed the new task")
	}
}

func TestWorkerReplayReadCompletion(t *testing.T) {
	for _, test := range []struct {
		name   string
		read   ReplayRead
		status string
	}{
		{"terminal", ReplayRead{Batch: replayBatch(`{"type":"worker_transcript_end","status":"completed","complete":true}`)}, "completed"},
		{"terminal-error", ReplayRead{Batch: replayBatch(`{"type":"worker_transcript_end","status":"completed","complete":true,"error":"io_error"}`)}, "Incomplete recording"},
		{"descriptor", ReplayRead{Descriptor: Transcript{Incomplete: true, Error: "size_limit"}}, "size_limit"},
		{"read-error", ReplayRead{Err: errors.New("missing file")}, "不可用"},
		{"grace-expired", ReplayRead{TaskEnded: true}, "without a terminal record"},
		{"partial-terminal", ReplayRead{Batch: replayBatch(`{"type":"worker_transcript_end","status":"cancelled","complete":false}`), Partial: true}, "incomplete trailing record"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var replay Replay
			replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"agent_start"}`)})
			if !replay.View().Busy {
				t.Fatal("fixture is not active")
			}
			if !replay.Apply(test.read) || replay.View().Busy || !replay.Stopped || !strings.Contains(replay.Status, test.status) {
				t.Fatalf("completion did not invalidate activity: %+v", replay)
			}
			if replay.Apply(ReplayRead{}) {
				t.Fatal("unchanged polling invalidated the view")
			}
		})
	}
}

func TestWorkerReplayDrainsBeforeIncompleteStop(t *testing.T) {
	for _, descriptor := range []Transcript{{}, {Incomplete: true, Error: "size_limit"}} {
		var replay Replay
		replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"agent_start"}`)})
		replay.Apply(ReplayRead{Batch: Batch{More: true}, Descriptor: descriptor, TaskEnded: true, Partial: true})
		if replay.Stopped || !replay.View().Busy {
			t.Fatal("stopped before draining remaining records")
		}
		replay.Apply(ReplayRead{Descriptor: descriptor, TaskEnded: true, Partial: true})
		if !replay.Stopped || replay.View().Busy || !strings.Contains(replay.Status, "incomplete trailing record") {
			t.Fatal("missing terminal record was not marked incomplete")
		}
	}
}
