//go:build unix

package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func heartbeatPeer(t *testing.T, s *Server) (*terminal, *peer) {
	t.Helper()
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	term := s.terminal
	if term == nil {
		t.Fatal("terminal not started")
	}
	term.mu.Lock()
	defer term.mu.Unlock()
	if len(term.peers) == 0 {
		t.Fatal("no connected peer")
	}
	return term, term.peers[len(term.peers)-1]
}

// Use bounded test intervals with the real server reader, writer and PTY.
func shortHeartbeat(t *testing.T, p *peer, timeout time.Duration) (context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); p.keepAlive(ctx, 20*time.Millisecond, timeout) }()
	t.Cleanup(func() { cancel(); awaitHeartbeat(t, done, "heartbeat cleanup") })
	return cancel, done
}
func awaitHeartbeat(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", what)
	}
}
func TestHeartbeatApplicationPongDoesNotEnterPTY(t *testing.T) {
	s, h, _ := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	term, p := heartbeatPeer(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	for {
		kind, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "INPUT:") {
			t.Fatalf("heartbeat leaked into PTY: %q", data)
		}
		if kind == websocket.MessageText && string(data) == `{"type":"pong"}` {
			break
		}
	}
	sendInput(t, c, []byte("AFTER_HEARTBEAT"))
	readUntil(t, c, fmt.Sprintf("INPUT:%x", []byte("AFTER_HEARTBEAT")))
	after, peer := heartbeatPeer(t, s)
	if after != term || peer != p {
		t.Fatal("heartbeat changed terminal/peer")
	}
}
func TestHeartbeatResponsivePeerAndParallelConnection(t *testing.T) {
	s, h, _ := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	term, p := heartbeatPeer(t, s)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	cancel, heartbeatDone := shortHeartbeat(t, p, 150*time.Millisecond)
	select {
	case <-p.stopped:
		t.Fatal("responsive peer disconnected")
	case <-time.After(250 * time.Millisecond):
	}
	c2 := dialTerminal(t, s, h)
	readUntil(t, c2, "PID:")
	current, newPeer := heartbeatPeer(t, s)
	if current != term || newPeer == p {
		t.Fatal("joining changed PTY or failed to add a peer")
	}
	sendInput(t, c, []byte("FIRST_STILL_LIVE"))
	readUntil(t, c2, fmt.Sprintf("INPUT:%x", []byte("FIRST_STILL_LIVE")))
	sendInput(t, c2, []byte("SECOND_LIVE"))
	readUntil(t, c2, fmt.Sprintf("INPUT:%x", []byte("SECOND_LIVE")))
	select {
	case <-p.stopped:
		t.Fatal("joining stopped original peer")
	default:
	}
	cancel()
	awaitHeartbeat(t, heartbeatDone, "canceled heartbeat")
	_ = c.CloseNow()
	awaitHeartbeat(t, readDone, "closed reader")
	sendInput(t, c2, []byte("SURVIVED"))
	readUntil(t, c2, fmt.Sprintf("INPUT:%x", []byte("SURVIVED")))
}
func TestHeartbeatTimeoutKeepsPTYAlive(t *testing.T) {
	s, h, log := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	term, p := heartbeatPeer(t, s)
	pid := term.cmd.Process.Pid
	// No reader means protocol Ping gets no Pong, as for a dead browser.
	_, done := shortHeartbeat(t, p, 80*time.Millisecond)
	awaitHeartbeat(t, done, "missing-pong timeout")
	awaitHeartbeat(t, p.stopped, "unresponsive peer stop")
	select {
	case <-term.done:
		t.Fatal("timeout killed TUI")
	default:
	}
	c2 := dialTerminal(t, s, h)
	readUntil(t, c2, "PID:")
	current, _ := heartbeatPeer(t, s)
	if current != term || current.cmd.Process.Pid != pid || strings.Count(logContents(t, log), "START:") != 1 {
		t.Fatal("timeout reconnect restarted TUI")
	}
	sendInput(t, c2, []byte("SURVIVED_TIMEOUT"))
	readUntil(t, c2, fmt.Sprintf("INPUT:%x", []byte("SURVIVED_TIMEOUT")))
}
func TestHeartbeatCancellationAndServerClose(t *testing.T) {
	s, h, _ := terminalServer(t)
	c := dialTerminal(t, s, h)
	readUntil(t, c, "PID:")
	term, p := heartbeatPeer(t, s)
	cancel, done := shortHeartbeat(t, p, time.Minute)
	time.Sleep(80 * time.Millisecond)
	cancel()
	awaitHeartbeat(t, done, "pending ping cancellation")
	select {
	case <-term.done:
		t.Fatal("heartbeat cancellation killed TUI")
	default:
	}
	c2 := dialTerminal(t, s, h)
	readUntil(t, c2, `"type":"ready"`)
	closed := make(chan struct{})
	go func() { _ = s.Close(); close(closed) }()
	awaitHeartbeat(t, closed, "host close with heartbeats")
	awaitHeartbeat(t, term.done, "host PTY shutdown")
}
