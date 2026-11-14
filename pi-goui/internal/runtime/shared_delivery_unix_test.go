//go:build unix

package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func deliveryBroker(t *testing.T) (*sessionBroker, *brokerSession, Config) {
	t.Helper()
	agent := t.TempDir()
	root := filepath.Join(testSharedRoot(t, agent), "executors", strings.Repeat("a", 64))
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	key, file, err := sharedSessionIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	b := &sessionBroker{root: root, sessions: map[string]*brokerSession{}, clients: map[string]*brokerPeer{}}
	s := &brokerSession{broker: b, key: key, file: file, clients: map[string]*brokerPeer{}, uiState: map[string]json.RawMessage{}, pendingUI: map[string]json.RawMessage{}}
	b.sessions[key] = s
	return b, s, Config{AgentDir: agent, SessionFile: file}
}

func deliveryWriter(p *brokerPeer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.writeLoop()
	}()
	return done
}

func readDeliveryFrame(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if err := json.Unmarshal(line, &frame); err != nil {
		t.Fatal(err)
	}
	return frame
}

func deliveryPeer(t *testing.T) (*brokerPeer, net.Conn) {
	t.Helper()
	conn, client := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	p := &brokerPeer{conn: conn, out: make(chan json.RawMessage, 256), done: make(chan struct{}), finishing: make(chan struct{}), ctx: ctx, cancel: cancel}
	t.Cleanup(func() { p.stop(); client.Close() })
	return p, client
}

func TestBrokerReplayDoesNotOverflowBeforeWriter(t *testing.T) {
	for _, count := range []int{255, 256, 257, 600} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, s, cfg := deliveryBroker(t)
			for i := 1; i < count; i++ {
				id := fmt.Sprint(i)
				raw := brokerJSON(map[string]any{"type": "extension_ui_request", "id": id, "method": "confirm"})
				if i%2 == 0 {
					s.uiState[id] = raw
				} else {
					s.pendingUI[id] = raw
				}
			}
			conn, client := net.Pipe()
			defer client.Close()
			defer conn.Close()
			p, _, err := b.attach(cfg, conn)
			if err != nil {
				t.Fatal(err)
			}
			defer p.stop()
			select {
			case <-p.done:
				t.Fatal("initial replay stopped peer before its writer could start")
			default:
			}
			// The first live frame must remain behind the entire replay.
			p.send(json.RawMessage(`{"type":"live_after_replay"}`))
			writerDone := deliveryWriter(p)
			defer func() { p.stop(); <-writerDone }()
			_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
			reader := bufio.NewReader(client)
			seen := map[string]bool{}
			for i := 0; i < count; i++ {
				frame := readDeliveryFrame(t, reader)
				if i == 0 {
					if frame["type"] != "goui_session_snapshot" {
						t.Fatalf("first replay frame = %v", frame)
					}
				} else if frame["type"] != "extension_ui_request" || seen[frame["id"].(string)] {
					t.Fatalf("missing or duplicate replay frame: %v", frame)
				} else {
					seen[frame["id"].(string)] = true
				}
			}
			if frame := readDeliveryFrame(t, reader); frame["type"] != "live_after_replay" {
				t.Fatalf("live frame out of order: %v", frame)
			}
		})
	}
}

func TestBrokerBackendEndAllowsQueuedDelivery(t *testing.T) {
	b, s, _ := deliveryBroker(t)
	conn, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	p := &brokerPeer{id: "peer", conn: conn, ctx: ctx, cancel: cancel, done: make(chan struct{}), finishing: make(chan struct{}), out: make(chan json.RawMessage, 256), session: s}
	defer p.stop()
	s.clients[p.id] = p
	b.clients[p.id] = p
	events := make(chan Event, 2)
	s.backend = &Client{events: events, exitErr: errors.New("fixture backend failed")}
	events <- Event{Data: json.RawMessage(`{"type":"message_end"}`)}
	events <- Event{Err: errors.New("fixture backend failed")}
	close(events)
	s.events()
	select {
	case <-p.done:
		t.Fatal("backend end closed the socket before queued events could be delivered")
	default:
	}
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(client)
	if frame := readDeliveryFrame(t, reader); frame["type"] != "message_end" {
		t.Fatalf("lost tail event: %v", frame)
	}
	if frame := readDeliveryFrame(t, reader); frame["message"] != "fixture backend failed" {
		t.Fatalf("lost backend error: %v", frame)
	}
	frame := readDeliveryFrame(t, reader)
	if frame["type"] != "goui_rpc_error" || frame["fatal"] != true || !strings.Contains(frame["message"].(string), "fixture backend failed") {
		t.Fatalf("missing terminal reason: %v", frame)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("connection remained open after terminal frame")
	}
}

func TestBrokerBackendNormalExitReportsTerminalReason(t *testing.T) {
	b, s, _ := deliveryBroker(t)
	p, client := deliveryPeer(t)
	p.id, p.session = "normal-exit", s
	s.clients[p.id], b.clients[p.id] = p, p
	events := make(chan Event)
	close(events)
	s.backend = &Client{events: events, exitErr: io.EOF}
	s.events()
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	frame := readDeliveryFrame(t, bufio.NewReader(client))
	if frame["fatal"] != true || frame["message"] != "pi backend stopped: EOF" {
		t.Fatalf("normal backend exit reason missing: %v", frame)
	}
}

func TestBrokerFinishingPeerRejectsCommands(t *testing.T) {
	p, _ := deliveryPeer(t)
	p.session = &brokerSession{} // A command must not reach the absent backend.
	p.finish(errors.New("backend closed"))
	written := make(chan struct{})
	(&sessionBroker{}).command(p, map[string]any{"type": "prompt", "id": "late", "message": "must not run"}, written)
	select {
	case <-written:
	default:
		t.Fatal("rejected command did not release the transaction fence")
	}
	if p.backlog.count != 0 {
		t.Fatal("finishing peer accepted a command response")
	}
}

type deliveryDeadlineConn struct {
	net.Conn
	mu        sync.Mutex
	deadlines []time.Time
}

func (c *deliveryDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadlines = append(c.deadlines, deadline)
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(deadline)
}

func TestBrokerPeerDrainUsesSingleDeadline(t *testing.T) {
	p, client := deliveryPeer(t)
	conn := &deliveryDeadlineConn{Conn: p.conn}
	p.conn = conn
	for i := 0; i < 10; i++ {
		p.send(json.RawMessage(`{"type":"tail"}`))
	}
	p.finish(errors.New("backend closed"))
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(client)
	for i := 0; i < 11; i++ {
		readDeliveryFrame(t, reader)
	}
	<-writerDone
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.deadlines) != 11 {
		t.Fatalf("write deadlines = %d, want 11", len(conn.deadlines))
	}
	for _, deadline := range conn.deadlines {
		if !deadline.Equal(p.finishDeadline) {
			t.Fatalf("drain extended its total deadline: %v, want %v", deadline, p.finishDeadline)
		}
	}
}

func TestBrokerPeerOverflowReportsTerminalReason(t *testing.T) {
	p, client := deliveryPeer(t)
	frameAt := func(i int) json.RawMessage {
		return brokerJSON(map[string]any{"type": "event", "sequence": fmt.Sprintf("%06d", i), "padding": strings.Repeat("x", 64*1024)})
	}
	accepted := sharedBacklogBytes / (len(frameAt(0)) + backlogEventOverhead)
	for i := 0; i <= accepted; i++ {
		p.send(frameAt(i))
	}
	if p.ctx.Err() == nil {
		t.Fatal("overflow did not cancel command admission")
	}
	p.send(json.RawMessage(`{"type":"after_overflow"}`))
	p.finish(errors.New("later reason must not replace overflow"))
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(client)
	for i := 0; i < accepted; i++ {
		if frame := readDeliveryFrame(t, reader); frame["sequence"] != fmt.Sprintf("%06d", i) {
			t.Fatalf("queued frame lost or reordered at %d: %v", i, frame)
		}
	}
	frame := readDeliveryFrame(t, reader)
	if frame["fatal"] != true || !strings.Contains(frame["message"].(string), "backlog exceeded (broker:") {
		t.Fatalf("overflow closed without explicit terminal reason: %v", frame)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("frames accepted after overflow")
	}
}

func TestBrokerPeerSmallBurstPreservesFIFO(t *testing.T) {
	p, client := deliveryPeer(t)
	for i := 0; i < 4096; i++ {
		p.send(brokerJSON(map[string]any{"type": "unknown_event", "sequence": i}))
	}
	if p.ctx.Err() != nil || p.backlog.count != 4096 {
		t.Fatalf("small burst disconnected or lost frames: count=%d err=%v", p.backlog.count, p.ctx.Err())
	}
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(client)
	for i := 0; i < 4096; i++ {
		if frame := readDeliveryFrame(t, reader); frame["sequence"] != float64(i) || frame["type"] != "unknown_event" {
			t.Fatalf("event %d lost or reordered: %v", i, frame)
		}
	}
	if p.ctx.Err() != nil {
		t.Fatal("burst closed a healthy peer")
	}
}

func TestBrokerSlowPeerDoesNotBlockHealthyPeer(t *testing.T) {
	slow, _ := deliveryPeer(t)
	healthy, client := deliveryPeer(t)
	s := &brokerSession{clients: map[string]*brokerPeer{"slow": slow, "healthy": healthy}}
	writerDone := deliveryWriter(healthy)
	defer func() { healthy.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(client)
	padding := strings.Repeat("x", 64*1024)
	for i := 0; i < 270; i++ {
		s.broadcast(brokerJSON(map[string]any{"type": "unknown_event", "sequence": i, "padding": padding}))
		if frame := readDeliveryFrame(t, reader); frame["sequence"] != float64(i) {
			t.Fatalf("healthy peer lost event %d", i)
		}
	}
	if slow.ctx.Err() == nil || healthy.ctx.Err() != nil {
		t.Fatalf("slow peer isolation failed: slow=%v healthy=%v", slow.ctx.Err(), healthy.ctx.Err())
	}
}

func TestBrokerPeerFinishIsBoundedWithoutReader(t *testing.T) {
	for _, startWriter := range []bool{false, true} {
		t.Run(fmt.Sprint(startWriter), func(t *testing.T) {
			t.Parallel()
			p, _ := deliveryPeer(t)
			p.send(json.RawMessage(`{"type":"blocked"}`))
			var writerDone <-chan struct{}
			if startWriter {
				writerDone = deliveryWriter(p)
				defer func() { p.stop(); <-writerDone }()
			}
			start := time.Now()
			p.finish(errors.New("backend closed"))
			if time.Since(start) > time.Second {
				t.Fatal("finishing blocked on socket I/O")
			}
			select {
			case <-p.done:
			case <-time.After(brokerPeerWriteTimeout + 3*time.Second):
				t.Fatal("peer exceeded the total drain deadline")
			}
			if startWriter {
				select {
				case <-writerDone:
				case <-time.After(time.Second):
					t.Fatal("blocked writer leaked after drain deadline")
				}
			}
		})
	}
}

func TestBrokerPeerConcurrentSendAndFinish(t *testing.T) {
	p, client := deliveryPeer(t)
	var sends sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 16; i++ {
		sends.Add(1)
		go func(id int) {
			defer sends.Done()
			<-start
			for sequence := 0; sequence < 8; sequence++ {
				p.send(brokerJSON(map[string]any{"type": "event", "sender": id, "sequence": sequence}))
			}
		}(i)
	}
	sends.Add(1)
	go func() {
		defer sends.Done()
		<-start
		p.finish(errors.New("concurrent finish"))
	}()
	close(start)
	sends.Wait()
	queued := p.backlog.count
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(client)
	last := map[float64]float64{}
	for i := 0; i < queued; i++ {
		frame := readDeliveryFrame(t, reader)
		id, sequence := frame["sender"].(float64), frame["sequence"].(float64)
		if previous, ok := last[id]; (ok && sequence != previous+1) || (!ok && sequence != 0) {
			t.Fatalf("sender sequence lost or reordered: %v", frame)
		}
		last[id] = sequence
	}
	if frame := readDeliveryFrame(t, reader); frame["fatal"] != true || frame["message"] != "concurrent finish" {
		t.Fatalf("terminal frame missing or reordered: %v", frame)
	}
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("connection accepted data after finish")
	}
}

func TestBrokerPeerGoneDuringDrainStopsWriter(t *testing.T) {
	p, client := deliveryPeer(t)
	p.send(json.RawMessage(`{"type":"tail"}`))
	p.finish(errors.New("backend closed"))
	client.Close()
	writerDone := deliveryWriter(p)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not stop after peer disappeared")
	}
}
