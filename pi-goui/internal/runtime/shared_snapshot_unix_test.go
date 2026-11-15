//go:build unix

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSharedSnapshotResponseIsFencedInBackendEventOrder(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "snapshot-rpc")
	// No timing sleeps: command completion is the barrier. The broker event
	// consumer deliberately cannot run until readLoop has received the reply.
	script := `#!/usr/bin/env python3
import json, sys
for line in sys.stdin:
 c=json.loads(line)
 if c['type']=='get_state':
  print(json.dumps({'type':'response','id':c['id'],'success':True,'data':{'sessionId':'late','isStreaming':False}}),flush=True)
 elif c['type']=='get_entries':
  print(json.dumps({'type':'agent_start'}),flush=True)
  print(json.dumps({'type':'message_update','assistantMessageEvent':{'type':'text_delta','contentIndex':0,'delta':'before'}}),flush=True)
  print(json.dumps({'type':'response','id':c['id'],'success':True,'data':{'entries':[]}}),flush=True)
  print(json.dumps({'type':'message_update','assistantMessageEvent':{'type':'text_delta','contentIndex':0,'delta':'after'}}),flush=True)
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	backend, err := startDirect(Config{Bin: bin, Cwd: t.TempDir(), NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	b, s, _ := deliveryBroker(t)
	s.backend, s.inFlight = backend, map[string]int{}
	p, frontend := deliveryPeer(t)
	p.id, p.session = "viewer", s
	s.clients[p.id], b.clients[p.id] = p, p
	writer := deliveryWriter(p)
	defer func() { p.stop(); <-writer }()
	commandDone := make(chan struct{})
	go func() {
		b.command(p, map[string]any{"type": "goui_get_snapshot", "id": "snapshot", "request": 7}, make(chan struct{}))
		close(commandDone)
	}()
	select {
	case <-commandDone:
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot reply waited on event consumer")
	}
	go s.events()
	_ = frontend.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(frontend)
	var sequence []string
	for len(sequence) < 3 {
		frame := readDeliveryFrame(t, reader)
		switch frame["type"] {
		case "message_update":
			sequence = append(sequence, frame["assistantMessageEvent"].(map[string]any)["delta"].(string))
		case "goui_history_snapshot":
			if frame["request"] != float64(7) || frame["state"].(map[string]any)["isStreaming"] != true {
				t.Fatalf("bad snapshot fence: %v", frame)
			}
			sequence = append(sequence, "snapshot")
		}
	}
	if strings.Join(sequence, ",") != "before,snapshot,after" {
		t.Fatal(sequence)
	}
}

func TestSharedSnapshotFrameBudgetIncludesStateAndEnvelope(t *testing.T) {
	// Real get_entries/get_state fields: each native response is under 64 MiB,
	// but their merged history fence exceeds the old one-response allowance.
	entries := snapshotJSON(map[string]any{"entries": []any{map[string]any{
		"type": "message", "id": "large", "message": map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": strings.Repeat("<", nativeFrameLimit-2048)}},
		},
	}}, "leafId": "large"})
	state := snapshotJSON(map[string]any{"sessionId": "large", "sessionName": strings.Repeat("<", 4096), "isCompacting": false})
	for _, data := range []json.RawMessage{entries, state} {
		native := snapshotJSON(map[string]any{"type": "response", "id": "query", "command": "get_entries", "success": true, "data": data})
		if len(native) >= nativeFrameLimit {
			t.Fatalf("invalid native fixture: %d", len(native))
		}
	}
	frame := historySnapshotFrame(^uint64(0), state, entries)
	if len(frame) <= nativeFrameLimit+workingFrameAllowance || len(frame) >= sharedFrameLimit || !json.Valid(frame) {
		t.Fatalf("bad merged frame size: %d", len(frame))
	}
	if overhead := len(frame) - len(state) - len(entries); overhead >= workingFrameAllowance {
		t.Fatalf("snapshot envelope exceeded its allowance: %d", overhead)
	}
	// HTML characters must not be re-escaped: that can multiply either legal
	// native response's size rather than merely add envelope overhead.
	if bytes.Contains(frame, []byte(`\u003c`)) {
		t.Fatal("native text expanded during framing")
	}
	before, after := json.RawMessage(`{"type":"message_update","delta":"before"}`), json.RawMessage(`{"type":"message_update","delta":"after"}`)
	t.Run("client_transport", func(t *testing.T) {
		client, conn := terminalTestClient(t)
		ctx, cancel := context.WithCancel(context.Background())
		peer := &brokerPeer{conn: conn, ctx: ctx, cancel: cancel, done: make(chan struct{}), finishing: make(chan struct{}), out: make(chan json.RawMessage, 256)}
		writerDone := deliveryWriter(peer)
		t.Cleanup(func() { peer.stop(); <-writerDone })
		for _, want := range []json.RawMessage{before, frame, after} {
			peer.send(want)
			got := <-client.Events()
			if got.Err != nil || !bytes.Equal(got.Data, want) {
				t.Fatalf("shared client rejected or reordered a legal frame: bytes=%d want=%d err=%v", len(got.Data), len(want), got.Err)
			}
		}
	})
	peer, _ := deliveryPeer(t)
	for _, want := range []json.RawMessage{before, frame, after} {
		peer.send(want)
		select {
		case <-peer.finishing:
			t.Fatal("legal frame rejected by the actual peer send path")
		default:
		}
		if got := <-peer.out; !bytes.Equal(got, want) {
			t.Fatal("snapshot/delta FIFO order changed")
		}
		peer.advance()
	}
	peer.send(frame)
	if peer.backlog.bytes != len(frame)+backlogEventOverhead {
		t.Fatal("large frame escaped backlog accounting")
	}
	peer.send(after)
	select {
	case <-peer.finishing:
	default:
		t.Fatal("large singleton accepted a follower beyond the queue budget")
	}
	// A syntactically valid protocol frame at the exclusive new limit is still
	// explicitly rejected, even by an otherwise empty peer.
	over := bytes.Repeat([]byte{'x'}, sharedFrameLimit)
	prefix := []byte(`{"type":"goui_history_snapshot","request":1,"state":{"sessionName":"`)
	suffix := []byte(`"},"entries":{"entries":[]}}`)
	copy(over, prefix)
	copy(over[len(over)-len(suffix):], suffix)
	if !json.Valid(over) {
		t.Fatal("invalid over-limit fixture")
	}
	tooLarge, _ := deliveryPeer(t)
	tooLarge.send(over)
	select {
	case <-tooLarge.finishing:
	default:
		t.Fatal("frame beyond the explicit snapshot budget was accepted")
	}
	if tooLarge.backlog.count != 0 || !bytes.Contains(tooLarge.terminal, []byte("frame_limit=")) {
		t.Fatal("oversize frame lacked an explicit rejection diagnostic")
	}
}

func TestSharedSnapshotRejectsUnboundedRequestEnvelope(t *testing.T) {
	for name, request := range map[string]any{"missing": nil, "zero": 0, "negative": -1, "text": strings.Repeat("x", 2048), "object": map[string]any{"value": 1}} {
		t.Run(name, func(t *testing.T) {
			b, s, _ := deliveryBroker(t)
			s.backend, s.inFlight = &Client{}, map[string]int{}
			peer, _ := deliveryPeer(t)
			peer.session, peer.id = s, "invalid-request"
			b.command(peer, map[string]any{"type": "goui_get_snapshot", "id": "invalid", "request": request}, make(chan struct{}))
			var response struct {
				Success bool
				Error   string
			}
			if err := json.Unmarshal(<-peer.out, &response); err != nil || response.Success || !strings.Contains(response.Error, "uint64 request") {
				t.Fatalf("unbounded snapshot envelope accepted: %+v, %v", response, err)
			}
		})
	}
}

func TestLateAttachReplaysUnfinishedMessageOnly(t *testing.T) {
	b, s, cfg := deliveryBroker(t)
	s.retainLiveMessage(json.RawMessage(`{"type":"message_start","message":{"role":"assistant","content":[{"type":"text","text":"prefix"}]}}`))
	s.retainLiveMessage(json.RawMessage(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"+","contentIndex":0}}`))
	conn, client := net.Pipe()
	defer conn.Close()
	defer client.Close()
	p, _, err := b.attach(cfg, conn)
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()
	if len(p.initial) != 3 || !strings.Contains(string(p.initial[1]), "prefix") || !strings.Contains(string(p.initial[2]), "text_delta") {
		t.Fatalf("missing initial prefix: %s", p.initial)
	}
	// Full native message updates replace the replay, rather than applying the
	// full message and then duplicating its embedded delta.
	s.retainLiveMessage(json.RawMessage(`{"type":"message_update","message":{"role":"assistant","content":"prefix++"},"assistantMessageEvent":{"type":"text_delta","delta":"+"}}`))
	if s.liveReplay.count != 1 || strings.Contains(string(s.liveReplay.first()), "text_delta") {
		t.Fatal("full checkpoint duplicated its delta")
	}
	s.retainLiveMessage(json.RawMessage(`{"type":"message_end"}`))
	if s.liveReplay.count != 0 || s.liveReplayErr != nil {
		t.Fatal("committed message remained in replay")
	}
}

func TestLiveReplayOverflowIsExplicitAndRecoversAtCheckpoint(t *testing.T) {
	s := &brokerSession{}
	raw := brokerJSON(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": strings.Repeat("x", sharedBacklogBytes/2)}})
	for i := 0; i < 3; i++ {
		s.retainLiveMessage(raw)
	}
	if s.liveReplayErr == nil || s.liveReplay.count != 0 {
		t.Fatal("live replay silently exceeded its byte budget")
	}
	s.retainLiveMessage(json.RawMessage(`{"type":"message_update","message":{"role":"assistant","content":"checkpoint"}}`))
	if s.liveReplayErr != nil || s.liveReplay.count != 1 {
		t.Fatal("full checkpoint did not restore replay")
	}
}
