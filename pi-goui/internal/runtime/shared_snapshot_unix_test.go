//go:build unix

package runtime

import (
	"bufio"
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
