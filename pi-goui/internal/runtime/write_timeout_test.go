package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Read one byte, then wait for permission to drain the pipe. The handshake
// proves the parent's first large write has started before a test cancels it.
func TestRPCWriteChild(t *testing.T) {
	gate := os.Getenv("GOUI_RPC_WRITE_GATE")
	if gate == "" {
		return
	}
	var first [1]byte
	if _, err := io.ReadFull(os.Stdin, first[:]); err != nil {
		os.Exit(2)
	}
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(map[string]any{"type": "write_started"})
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	scanner := bufio.NewScanner(io.MultiReader(bytes.NewReader(first[:]), os.Stdin))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	for scanner.Scan() {
		var command struct {
			Type, ID, Value string
		}
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			os.Exit(3)
		}
		log, err := os.OpenFile(os.Getenv("GOUI_RPC_WRITE_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			os.Exit(4)
		}
		_, _ = log.WriteString(command.Type + "\n")
		_ = log.Close()
		_ = encoder.Encode(map[string]any{"type": "response", "id": command.ID, "success": true, "data": map[string]any{"value": command.Value}})
	}
	os.Exit(0)
}

func writeTestClient(t *testing.T) (*Client, string, string) {
	t.Helper()
	root := t.TempDir()
	gate, log := filepath.Join(root, "drain"), filepath.Join(root, "received")
	t.Setenv("GOUI_RPC_WRITE_GATE", gate)
	t.Setenv("GOUI_RPC_WRITE_LOG", log)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(Config{Bin: bin, Args: []string{"-test.run=^TestRPCWriteChild$", "--"}, AgentDir: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, gate, log
}

func waitWriteStarted(t *testing.T, c *Client) {
	t.Helper()
	select {
	case event := <-c.Events():
		if !bytes.Contains(event.Data, []byte(`"write_started"`)) {
			t.Fatalf("missing write handshake: %+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write did not start")
	}
}

func releaseWrite(t *testing.T, gate string) {
	t.Helper()
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestStartedWriteCancellationInvalidatesTransport(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			c, gate, log := writeTestClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- c.Send(ctx, map[string]any{"type": "expired", "value": strings.Repeat("x", 4<<20)}) }()
			waitWriteStarted(t, c)
			want := context.DeadlineExceeded
			if !deadline {
				want = context.Canceled
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("send error = %v, want %v", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("send ignored cancellation")
			}
			select {
			case <-c.stop:
			default:
				t.Fatal("partially written transport remained usable")
			}
			// Even if the backend resumes immediately after Send returns, it
			// must not receive the rest of the cancelled JSONL command.
			releaseWrite(t, gate)
			next, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			if err := c.Send(next, map[string]any{"type": "later"}); err == nil || next.Err() != nil {
				t.Fatalf("subsequent send did not fail promptly: %v", err)
			}
			_ = c.Close()
			select {
			case <-c.writeDone:
			default:
				t.Fatal("writer survived close")
			}
			if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("cancelled command reached the backend: %v", err)
			}
		})
	}
}

func TestQueuedWriteCancellationKeepsTransportHealthy(t *testing.T) {
	c, gate, log := writeTestClient(t)
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	result := make(chan error, 1)
	go func() { result <- c.Send(first, map[string]any{"type": "first", "value": strings.Repeat("x", 4<<20)}) }()
	waitWriteStarted(t, c)
	queued, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := c.Send(queued, map[string]any{"type": "cancelled"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-c.stop:
		t.Fatal("cancelling a queued request killed the active writer")
	default:
	}
	releaseWrite(t, gate)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active write did not resume")
	}
	cancelFirst() // A completed write must not close the connection later.
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if _, err := c.Call(ctx, map[string]any{"type": "healthy", "value": "ok"}); err != nil {
		t.Fatal(err)
	}
	received, err := os.ReadFile(log)
	if err != nil || string(received) != "first\nhealthy\n" {
		t.Fatalf("unexpected delivered commands: %q (%v)", received, err)
	}
}

func TestWriteCancellationAndConcurrentClose(t *testing.T) {
	c, _, _ := writeTestClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- c.Send(ctx, map[string]any{"type": "cancelled", "value": strings.Repeat("x", 4<<20)})
	}()
	waitWriteStarted(t, c)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = c.Close() }()
	}
	cancel()
	closed := make(chan struct{})
	go func() { wg.Wait(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("concurrent cancellation/close deadlocked")
	}
	if err := <-result; err == nil {
		t.Fatal("cancelled partial write succeeded")
	}
}
