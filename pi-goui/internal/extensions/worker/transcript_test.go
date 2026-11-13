package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestTranscriptRPCContinuationAndNoFilesystemContract(t *testing.T) {
	d := Transcript{Version: 1, ID: transcriptTestID}
	calls := 0
	client := fakeRPC(func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		calls++
		if method != "worker.transcript" {
			t.Fatalf("method %q", method)
		}
		raw, _ := json.Marshal(params)
		var p map[string]json.RawMessage
		_ = json.Unmarshal(raw, &p)
		if len(p) != calls || p["descriptor"] == nil {
			t.Fatalf("non-opaque request: %s", raw)
		}
		if calls == 2 && string(p["cursor"]) != `"`+transcriptTestID+`"` {
			t.Fatalf("cursor missing: %s", raw)
		}
		return json.RawMessage(`{"cursor":"` + transcriptTestID + `","reset":true,"partial":true,"more":true,"records":[{"type":"worker_transcript_header","version":1,"id":"` + transcriptTestID + `","entries":[]}]}`), nil
	})
	c, b, err := (Cursor{}).Read(context.Background(), client, d)
	if err != nil || !b.Reset || !b.More || len(b.Records) != 1 || !c.Partial() {
		t.Fatalf("%+v %v", b, err)
	}
	_, _, err = c.Read(context.Background(), client, d)
	if err != nil || calls != 2 {
		t.Fatal(err)
	}
}
func TestTranscriptUnavailableInvalidAndCancellation(t *testing.T) {
	d := Transcript{Version: 1, ID: transcriptTestID}
	if _, _, err := (Cursor{}).Read(context.Background(), nil, d); err == nil {
		t.Fatal("missing backend accepted")
	}
	for _, raw := range []string{`{}`, `null`, `{"cursor":"../private"}`, `{"cursor":"` + transcriptTestID + `","records":[{}]}`} {
		client := fakeRPC(func(context.Context, string, any) (json.RawMessage, error) { return json.RawMessage(raw), nil })
		if _, _, err := (Cursor{}).Read(context.Background(), client, d); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	client := fakeRPC(func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (Cursor{}).Read(ctx, client, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	called := false
	client = fakeRPC(func(context.Context, string, any) (json.RawMessage, error) { called = true; return nil, nil })
	if _, _, err := (Cursor{}).Read(context.Background(), client, Transcript{Version: 1, ID: "../private"}); err == nil || called {
		t.Fatal("invalid descriptor sent")
	}
}
func TestTranscriptReplayFromRemoteRecords(t *testing.T) {
	client := fakeRPC(func(context.Context, string, any) (json.RawMessage, error) {
		return json.RawMessage(`{"cursor":"` + transcriptTestID + `","reset":true,"records":[{"type":"worker_transcript_header","version":1,"id":"` + transcriptTestID + `","entries":[]},{"type":"message_end","message":{"role":"user","content":"中文 🧪","timestamp":1}},{"type":"worker_transcript_end","status":"completed","complete":true}]}`), nil
	})
	_, b, err := (Cursor{}).Read(context.Background(), client, Transcript{Version: 1, ID: transcriptTestID})
	if err != nil {
		t.Fatal(err)
	}
	var replay Replay
	replay.Apply(ReplayRead{Batch: b})
	if !replay.Stopped || replay.Status != "completed" || len(replay.View().Items) != 1 || replay.View().Items[0].Text != "中文 🧪" {
		t.Fatalf("%+v", replay.View())
	}
}
