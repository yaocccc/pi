//go:build unix

package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The extra stdin line is a barrier: the test owns both the auxiliary path
// lock and (optionally) the response-delivery lock before the child replies.
func TestBrokerCommandExitChild(t *testing.T) {
	mode := os.Getenv("GOUI_COMMAND_EXIT_CHILD")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	var command map[string]any
	if json.Unmarshal(scanner.Bytes(), &command) != nil || !scanner.Scan() {
		os.Exit(3)
	}
	if mode == "reply" {
		response := map[string]any{"type": "response", "id": command["id"], "success": true, "data": map[string]any{"accepted": true}}
		_ = json.NewEncoder(os.Stdout).Encode(response)
	}
	fmt.Fprintln(os.Stderr, "fixture command exit diagnostic")
	os.Exit(7)
}

func TestBrokerResponseDrainBounded(t *testing.T) {
	_, s, _ := deliveryBroker(t)
	p, client := deliveryPeer(t)
	s.clients["stalled"] = p
	events := make(chan Event)
	close(events)
	s.backend = &Client{events: events}
	// Model a command that never gets scheduled, without leaving a blocked
	// goroutine behind. EOF must still close the peer's response window.
	s.responses, s.responsesDone = 1, make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); s.events() }()
	select {
	case <-done:
	case <-time.After(brokerPeerWriteTimeout + 2*time.Second):
		t.Fatal("response drain exceeded its finite deadline")
	}
	writerDone := deliveryWriter(p)
	defer func() { p.stop(); <-writerDone }()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	terminal := readDeliveryFrame(t, bufio.NewReader(client))
	if terminal["fatal"] != true {
		t.Fatalf("missing terminal error after response timeout: %v", terminal)
	}
}

func TestBrokerCommandResultBeforeExitAndPathRefresh(t *testing.T) {
	for _, mode := range []string{"reply", "no-reply"} {
		for _, pauseDelivery := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/paused=%v", mode, pauseDelivery), func(t *testing.T) {
				b, s, _ := deliveryBroker(t)
				t.Setenv("GOUI_COMMAND_EXIT_CHILD", mode)
				bin, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				backend, err := startDirect(Config{Bin: bin, Args: []string{"-test.run=^TestBrokerCommandExitChild$", "--"}, AgentDir: t.TempDir(), NoSession: true})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { backend.Close() })
				p, client := deliveryPeer(t)
				p.id, p.session = "command-exit", s
				s.backend, s.inFlight = backend, map[string]int{}
				s.clients[p.id], b.clients[p.id] = p, p
				written, commandDone := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(commandDone)
					b.command(p, map[string]any{"type": "prompt", "id": "final", "message": "offline fixture"}, written)
				}()
				select {
				case <-written:
				case <-time.After(3 * time.Second):
					t.Fatal("command was not written")
				}
				// Registration must not be part of the response-drain barrier.
				s.opMu.Lock()
				defer s.opMu.Unlock()
				if pauseDelivery {
					p.mu.Lock()
					defer func() {
						if pauseDelivery {
							p.mu.Unlock()
						}
					}()
				}
				if _, err := fmt.Fprintln(backend.stdin, "exit now"); err != nil {
					t.Fatal(err)
				}
				// Drain the real Client's event stream before starting the terminal
				// broker phase. This isolates EOF from the response goroutine, which
				// is still paused at p.send in the delayed-delivery case.
				for event := range backend.Events() {
					if event.Err != nil && !strings.Contains(event.Err.Error(), "fixture command exit diagnostic") {
						t.Fatalf("missing child diagnostic: %v", event.Err)
					}
				}
				eventsDone := make(chan struct{})
				go func() { defer close(eventsDone); s.events() }()
				if pauseDelivery {
					// Observe the EOF admission fence, not a guessed scheduling delay.
					fenced := make(chan struct{})
					go func() {
						for {
							b.mu.Lock()
							closed := s.closed
							b.mu.Unlock()
							if closed {
								close(fenced)
								return
							}
							runtime.Gosched()
						}
					}()
					select {
					case <-fenced:
					case <-time.After(3 * time.Second):
						t.Fatal("EOF did not establish the command fence")
					}
					select {
					case <-p.finishing:
						t.Fatal("peer finished before the admitted response was queued")
					default:
					}
					pauseDelivery = false
					p.mu.Unlock()
				}
				select {
				case <-eventsDone:
				case <-time.After(3 * time.Second):
					t.Fatal("terminal phase waited for auxiliary path refresh")
				}
				writerDone := deliveryWriter(p)
				defer func() { p.stop(); <-writerDone }()
				_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(client)
				response := readDeliveryFrame(t, reader)
				if response["type"] != "response" || response["id"] != "final" || response["success"] != (mode == "reply") {
					t.Fatalf("final command response lost: %v", response)
				}
				if mode == "reply" && response["data"].(map[string]any)["accepted"] != true {
					t.Fatalf("wrong determined result: %v", response)
				}
				terminal := readDeliveryFrame(t, reader)
				if terminal["fatal"] != true || !strings.Contains(fmt.Sprint(terminal["message"]), "fixture command exit diagnostic") {
					t.Fatalf("terminal diagnostic lost: %v", terminal)
				}
				if _, err := reader.ReadByte(); err == nil {
					t.Fatal("duplicate response or socket not closed")
				}
				// Deferred unlock lets a successful command finish registration;
				// cleanup waits for it before destroying the fixture backend.
				t.Cleanup(func() {
					select {
					case <-commandDone:
					case <-time.After(3 * time.Second):
						t.Error("command failed to finish after releasing auxiliary lock")
					}
				})
			})
		}
	}
}
