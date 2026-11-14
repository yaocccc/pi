package worker

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func replayBatch(records ...string) Batch {
	var batch Batch
	for _, record := range records {
		batch.Records = append(batch.Records, json.RawMessage(record))
	}
	return batch
}

func TestWorkerReplayInvalidHistoryPreservesReadableRecording(t *testing.T) {
	badHeaders := map[string]string{
		"missing-parent":  `{"type":"worker_transcript_header","entries":[{"type":"message","id":"a","parentId":"missing","message":{"role":"user","content":"bad"}}],"leafId":"a"}`,
		"duplicate-id":    `{"type":"worker_transcript_header","entries":[{"type":"message","id":"a","message":{"role":"user","content":"one"}},{"type":"message","id":"a","message":{"role":"user","content":"two"}}],"leafId":"a"}`,
		"invalid-message": `{"type":"worker_transcript_header","entries":[{"type":"message","id":"a","message":42}],"leafId":"a"}`,
	}
	for name, header := range badHeaders {
		for _, reset := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/initial", true: "/reset"}[reset], func(t *testing.T) {
				var replay Replay
				if reset {
					replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_end","message":{"role":"user","content":"keep readable","timestamp":1}}`)})
				}
				before := replay.View()
				batch := replayBatch(header, `{"type":"worker_transcript_end","status":"completed","complete":true}`)
				batch.Reset = reset
				replay.Apply(ReplayRead{Batch: batch})
				if replay.Err == nil || !replay.Stopped || !strings.Contains(replay.Status, "Incomplete") || !reflect.DeepEqual(replay.View().Items, before.Items) || replay.View().MessageCount != before.MessageCount {
					t.Fatalf("bad history erased content or hid error: %+v", replay)
				}
				replay.Apply(ReplayRead{Batch: replayBatch(`{"type":"worker_transcript_end","status":"completed","complete":true}`)})
				if !strings.Contains(replay.Status, "Incomplete") {
					t.Fatal("late completed record hid history error")
				}
				repaired := replayBatch(`{"type":"worker_transcript_header","entries":[]}`)
				repaired.Reset = true
				replay.Apply(ReplayRead{Batch: repaired})
				if replay.Err != nil || replay.Stopped || len(replay.View().Items) != 0 {
					t.Fatal("validated reset did not recover")
				}
			})
		}
	}
}

func TestWorkerReplayDetachedConcurrentEventsAndProjection(t *testing.T) {
	var original Replay
	original.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_start","message":{"role":"assistant","content":[],"timestamp":1}}`, `{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"plan","result":{"details":{"plan":[{"id":"a","goal":"Inspect","status":"in_progress"}]}}}`)})
	next := original.Detached()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 30; i++ {
			next.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"background"}}`)})
		}
		next.Apply(ReplayRead{Batch: replayBatch(`{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"plan","result":{"details":{"plan":[{"id":"a","goal":"Inspect","status":"completed"}]}}}`)})
	}()
	for i := 0; i < 30; i++ {
		original.Apply(ReplayRead{Batch: replayBatch(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","contentIndex":0,"delta":"foreground"}}`)})
		_ = original.View()
	}
	wg.Wait()
	if original.View().Plan[0].Status != "in_progress" || next.View().Plan[0].Status != "completed" {
		t.Fatal("detached replay shared extension projection")
	}
	for _, item := range original.View().Items {
		if strings.Contains(item.Text, "background") {
			t.Fatal("detached event changed original")
		}
	}
	// Empty polls must also retain the detached projection.
	empty := next.Detached()
	empty.Apply(ReplayRead{})
	if len(empty.View().Plan) != 1 || empty.View().Plan[0].Status != "completed" {
		t.Fatal("empty poll dropped extension metadata")
	}
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
