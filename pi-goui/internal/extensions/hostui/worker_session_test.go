package hostui

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/worker"
)

const replayTestID = "12345678-1234-4234-8234-123456789abc"

type replayTestClient struct {
	raw     json.RawMessage
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (c *replayTestClient) CallExtension(context.Context, string, any) (json.RawMessage, error) {
	if c.started != nil {
		c.once.Do(func() { close(c.started) })
		<-c.release
	}
	return c.raw, nil
}

func replayResponse(reset bool, records ...string) json.RawMessage {
	batch := worker.Batch{Reset: reset}
	for _, record := range records {
		batch.Records = append(batch.Records, json.RawMessage(record))
	}
	raw, err := json.Marshal(struct {
		worker.Batch
		Cursor string `json:"cursor"`
	}{batch, replayTestID})
	if err != nil {
		panic(err)
	}
	return raw
}

func replayViewer(raw json.RawMessage) (*WorkerSession, session.Item, *replayTestClient) {
	client := &replayTestClient{raw: raw}
	item := session.Item{Details: json.RawMessage(`{"tasks":[{"status":"running","transcript":{"version":1,"id":"` + replayTestID + `"}}]}`)}
	viewer, reason := NewWorkerSession(item, true, client)
	if reason != "" {
		panic(reason)
	}
	return viewer, item, client
}

func readReplay(t *testing.T, w *WorkerSession, item session.Item) WorkerTranscriptResult {
	t.Helper()
	w.Next = time.Time{}
	cmd, reason := w.Poll(item, time.Now())
	if cmd == nil || reason != "" {
		t.Fatalf("missing read: %s", reason)
	}
	return cmd().(WorkerTranscriptResult)
}

func TestWorkerPreparedReadKeepsOwnerResponsiveAndCommitsWithoutParsing(t *testing.T) {
	// A large inherited header and a separate large task event both need JSON
	// parsing. Neither may be parsed or copied by Receive on the UI thread.
	header := `{"type":"worker_transcript_header","entries":[{"type":"message","id":"u","message":{"role":"user","content":"` + strings.Repeat("inherited ", 1<<18) + `"}}]}`
	answer := `{"type":"message_end","message":{"role":"assistant","content":"` + strings.Repeat("task ", 1<<18) + `","timestamp":2}}`
	w, item, client := replayViewer(replayResponse(true, header, answer))
	gate := make(chan struct{})
	client.started, client.release = make(chan struct{}), gate
	cmd, _ := w.Poll(item, time.Now())
	results := make(chan WorkerTranscriptResult, 1)
	go func() { results <- cmd().(WorkerTranscriptResult) }()
	<-client.started
	if !w.Pending || len(w.Visible) != 0 || w.Replay.View().MessageCount != 0 {
		t.Fatal("blocked read changed the owner")
	}
	close(gate)
	var msg WorkerTranscriptResult
wait:
	for {
		select {
		case msg = <-results:
			break wait
		default:
			// These are the same owner reads used by rendering while a command
			// prepares its private replay. The race run checks actual overlap.
			if w.Replay.View().MessageCount != 0 || len(w.Visible) != 0 {
				t.Fatal("preparation mutated the visible replay")
			}
			runtime.Gosched()
		}
	}
	if msg.Err != nil || msg.replay.View().MessageCount != 2 || len(msg.visible) != 1 {
		t.Fatalf("history/event not prepared in command: %v", msg.Err)
	}
	// Destroy the raw input after preparation. Commit must not parse it again.
	msg.Batch.Records = []json.RawMessage{json.RawMessage(`not JSON`)}
	if _, reason := w.Receive(msg, &item); reason != "" {
		t.Fatal(reason)
	}
	if w.Pending || len(w.Visible) != 1 || !strings.HasPrefix(w.Visible[0].Text, "task ") || w.Replay.View().MessageCount != 2 {
		t.Fatal("prepared content was not committed intact")
	}
	revision := w.Revision
	w.Receive(msg, &item)
	if w.Revision != revision {
		t.Fatal("duplicate result committed twice")
	}
}

func TestWorkerPreparedReadRejectsStaleOwnership(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*WorkerSession, *session.Item, *WorkerTranscriptResult)
	}{
		{"owner", func(w *WorkerSession, _ *session.Item, m *WorkerTranscriptResult) { m.Owner = &WorkerSession{} }},
		{"token", func(w *WorkerSession, _ *session.Item, m *WorkerTranscriptResult) { m.job = &workerTranscriptJob{} }},
		{"revision", func(w *WorkerSession, _ *session.Item, _ *WorkerTranscriptResult) { w.Revision++ }},
		{"client", func(w *WorkerSession, _ *session.Item, _ *WorkerTranscriptResult) { w.Client = &replayTestClient{} }},
		{"descriptor", func(w *WorkerSession, _ *session.Item, _ *WorkerTranscriptResult) {
			w.Descriptor.ID = "22345678-1234-4234-8234-123456789abc"
		}},
		{"item", func(w *WorkerSession, item *session.Item, _ *WorkerTranscriptResult) {
			item.Details = json.RawMessage(strings.ReplaceAll(string(item.Details), replayTestID, "22345678-1234-4234-8234-123456789abc"))
		}},
		{"descriptor-metadata", func(w *WorkerSession, item *session.Item, _ *WorkerTranscriptResult) {
			item.Details = json.RawMessage(strings.ReplaceAll(string(item.Details), `"version":1`, `"incomplete":true,"version":1`))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w, item, _ := replayViewer(replayResponse(true, `{"type":"worker_transcript_header","entries":[]}`, `{"type":"message_end","message":{"role":"user","content":"stale","timestamp":1}}`))
			msg := readReplay(t, w, item)
			test.change(w, &item, &msg)
			w.Receive(msg, &item)
			if len(w.Visible) != 0 || w.Replay.View().MessageCount != 0 {
				t.Fatal("stale read overwrote owner")
			}
		})
	}
}

func TestWorkerPreparedBadResetRetainsReadableView(t *testing.T) {
	w, item, client := replayViewer(replayResponse(true, `{"type":"worker_transcript_header","entries":[]}`, `{"type":"message_end","message":{"role":"user","content":"keep readable","timestamp":1}}`))
	msg := readReplay(t, w, item)
	if _, reason := w.Receive(msg, &item); reason != "" {
		t.Fatal(reason)
	}
	before := w.Visible
	client.raw = replayResponse(true, `{"type":"worker_transcript_header","entries":[{"type":"message","id":"a","parentId":"missing","message":{"role":"user","content":"bad"}}],"leafId":"a"}`, `{"type":"worker_transcript_end","complete":true,"status":"completed"}`)
	msg = readReplay(t, w, item)
	if msg.Err == nil {
		t.Fatal("invalid native branch was not rejected during preparation")
	}
	_, reason := w.Receive(msg, &item)
	if reason == "" || !w.Unavailable(reason) || !reflect.DeepEqual(w.Visible, before) || !strings.Contains(w.Replay.Status, "保留已读取内容") {
		t.Fatal("bad reset erased or disguised the previous recording")
	}
	w.Receive(msg, &item)
	if !strings.Contains(w.Replay.Status, "保留已读取内容") {
		t.Fatal("late result hid incomplete status")
	}
}

func BenchmarkWorkerPreparedReplayStages(b *testing.B) {
	header := `{"type":"worker_transcript_header","entries":[{"type":"message","id":"u","message":{"role":"user","content":"` + strings.Repeat("x", 4<<20) + `"}}]}`
	raw := replayResponse(true, header, `{"type":"message_end","message":{"role":"assistant","content":"task answer","timestamp":2}}`)
	b.Run("prepare", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			w, item, _ := replayViewer(raw)
			cmd, _ := w.Poll(item, time.Now())
			if msg := cmd().(WorkerTranscriptResult); msg.Err != nil {
				b.Fatal(msg.Err)
			}
		}
	})
	b.Run("commit", func(b *testing.B) {
		w, item, _ := replayViewer(raw)
		cmd, _ := w.Poll(item, time.Now())
		msg := cmd().(WorkerTranscriptResult)
		if msg.Err != nil {
			b.Fatal(msg.Err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w.readJob, w.Pending, w.Revision = msg.job, true, msg.job.revision
			if _, reason := w.Receive(msg, nil); reason != "" {
				b.Fatal(reason)
			}
		}
	})
}
