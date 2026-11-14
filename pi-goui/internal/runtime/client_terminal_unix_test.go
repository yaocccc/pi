//go:build unix

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func terminalTestClient(t *testing.T) (*Client, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	c := &Client{stdin: client, stdout: client, shared: &sharedConnection{}, writes: make(chan *writeRequest, 32), events: make(chan Event), eventBatches: make(chan []Event), batchNotify: make(chan struct{}), incoming: make(chan Event, 32), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), writeDone: make(chan struct{}), closeDone: make(chan struct{}), pending: make(map[string]chan reply)}
	go c.readLoop()
	go c.writeLoop()
	go c.eventLoop()
	go func() { <-c.readDone; close(c.done) }()
	t.Cleanup(func() { server.Close(); c.Close() })
	_ = server.SetDeadline(time.Now().Add(5 * time.Second))
	return c, server
}

func TestBrokerClientPreservesBackendExit(t *testing.T) {
	b, s, _ := deliveryBroker(t)
	t.Setenv("GOUI_EXIT_DIAGNOSTIC_CHILD", "tail")
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := startDirect(Config{Bin: bin, Args: []string{"-test.run=^TestExitDiagnosticChild$", "--"}, AgentDir: t.TempDir(), NoSession: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	c, server := terminalTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	p := &brokerPeer{id: "peer", conn: server, ctx: ctx, cancel: cancel, done: make(chan struct{}), finishing: make(chan struct{}), out: make(chan json.RawMessage, 256), session: s}
	t.Cleanup(p.stop)
	s.backend = backend
	s.clients[p.id], b.clients[p.id] = p, p
	writerDone := deliveryWriter(p)
	s.events()
	seenTail := false
	for e := range c.Events() {
		if strings.Contains(string(e.Data), "message_end") {
			seenTail = true
		}
	}
	<-writerDone
	if !seenTail {
		t.Fatal("backend tail event was lost between broker and client")
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "exit status 7") || !strings.Contains(err.Error(), "fixture final stderr") {
		t.Fatalf("backend failure was lost between broker and client: %v", err)
	}
}

func TestSharedFatalErrorRetainedAfterEOF(t *testing.T) {
	c, server := terminalTestClient(t)
	go func() {
		defer server.Close()
		_, _ = fmt.Fprintln(server, `{"type":"goui_rpc_error","fatal":true,"message":"backend exit status 7: fixture stderr"}`)
	}()
	for range c.Events() {
	}
	want := "backend exit status 7: fixture stderr"
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("terminal failure = %v, want %q", err, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c.Call(ctx, map[string]any{"type": "get_state"}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("post-close call error = %v, want terminal reason", err)
	}
}

func TestSharedBacklogRetainsTerminalError(t *testing.T) {
	c, server := terminalTestClient(t)
	written := make(chan struct{})
	go func() {
		defer close(written)
		frame := `{"type":"message_update","padding":"` + strings.Repeat("x", 64*1024) + `"}`
		for i := 0; i < 300; i++ {
			if _, err := fmt.Fprintln(server, frame); err != nil {
				return
			}
		}
	}()
	// Do not consume Events until the client's bounded queue overflows.
	select {
	case <-c.stop:
	case <-time.After(5 * time.Second):
		t.Fatal("full shared queue did not stop client")
	}
	for range c.Events() {
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "shared event backlog exceeded (client:") || !strings.Contains(err.Error(), "budget=16777216") {
		t.Fatalf("lost backlog failure: %v", err)
	}
	<-written
}

func TestSharedSmallBurstDoesNotDisconnect(t *testing.T) {
	c, server := terminalTestClient(t)
	written := make(chan error, 1)
	go func() {
		for i := 0; i < 4096; i++ {
			if _, err := fmt.Fprintf(server, "{\"type\":\"unknown_event\",\"sequence\":%d}\n", i); err != nil {
				written <- err
				return
			}
		}
		written <- nil
	}()
	// No Events consumer runs until the complete burst has arrived.
	if err := <-written; err != nil {
		t.Fatalf("small event burst disconnected: %v", err)
	}
	for i := 0; i < 4096; i++ {
		select {
		case e, ok := <-c.Events():
			var value struct {
				Type     string
				Sequence int
			}
			if !ok || e.Err != nil || json.Unmarshal(e.Data, &value) != nil || value.Type != "unknown_event" || value.Sequence != i {
				t.Fatalf("event %d lost or reordered: %+v (%v)", i, e, ok)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("event %d was not delivered", i)
		}
	}
	if err := c.Err(); err != nil {
		t.Fatalf("burst ended a healthy connection: %v", err)
	}
}

func TestSharedSmallBurstBatchesAndTerminal(t *testing.T) {
	c, server := terminalTestClient(t)
	written := make(chan error, 1)
	go func() {
		defer server.Close()
		for i := 0; i < 4096; i++ {
			if _, err := fmt.Fprintf(server, "{\"type\":\"unknown_event\",\"sequence\":%d}\n", i); err != nil {
				written <- err
				return
			}
		}
		_, err := fmt.Fprintln(server, `{"type":"goui_rpc_error","fatal":true,"message":"batched backend exit status 7"}`)
		written <- err
	}()
	if err := <-written; err != nil {
		t.Fatalf("small event burst disconnected: %v", err)
	}
	next, largest, failures := 0, 0, 0
	for {
		batch, ok := readEventBatch(t, c)
		if !ok {
			break
		}
		largest = max(largest, len(batch))
		if len(batch) == 0 || len(batch) > eventBatchLimit {
			t.Fatalf("invalid batch size: %d", len(batch))
		}
		for _, e := range batch {
			if e.Err != nil {
				failures++
				if next != 4096 || !strings.Contains(e.Err.Error(), "batched backend exit status 7") {
					t.Fatal("terminal error preceded accepted data or lost its cause")
				}
				continue
			}
			var value struct{ Sequence int }
			if json.Unmarshal(e.Data, &value) != nil || value.Sequence != next {
				t.Fatalf("event %d lost or reordered: %s", next, e.Data)
			}
			next++
		}
	}
	if next != 4096 || largest != eventBatchLimit || failures != 1 {
		t.Fatalf("received=%d largest batch=%d failures=%d", next, largest, failures)
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "batched backend exit status 7") {
		t.Fatalf("lost shared terminal diagnosis: %v", err)
	}
}

func TestSharedReadFailureRetained(t *testing.T) {
	c, _ := terminalTestClient(t)
	if err := c.Err(); err != nil {
		t.Fatalf("active client error = %v", err)
	}
	_ = c.stdin.(net.Conn).SetReadDeadline(time.Now())
	for range c.Events() {
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "read pi stdout:") || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("read failure lost at close: %v", err)
	}
}

func TestSharedNonfatalErrorDoesNotEndStream(t *testing.T) {
	c, server := terminalTestClient(t)
	go func() {
		defer server.Close()
		_, _ = fmt.Fprintln(server, `{"type":"goui_rpc_error","message":"recoverable fixture warning"}`)
		_, _ = fmt.Fprintln(server, `{"type":"message_end"}`)
	}()
	seenWarning, seenTail := false, false
	for e := range c.Events() {
		if e.Err != nil && strings.Contains(e.Err.Error(), "recoverable fixture warning") {
			seenWarning = true
		}
		if strings.Contains(string(e.Data), "message_end") {
			seenTail = true
		}
	}
	if !seenWarning || !seenTail {
		t.Fatalf("warning=%v tail=%v", seenWarning, seenTail)
	}
	if err := c.Err(); err == nil || strings.Contains(err.Error(), "recoverable fixture warning") {
		t.Fatal("nonfatal warning became terminal failure")
	}
}
