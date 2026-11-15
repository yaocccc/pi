package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDirectEventQueueAppliesBackpressure(t *testing.T) {
	for _, mib := range []int{6, 20} {
		t.Run(fmt.Sprint(mib), func(t *testing.T) {
			c := eventLoopFixture(t, true)
			payload := json.RawMessage(strings.Repeat("x", mib<<20))
			accepted := make(chan int, 5)
			go func() {
				defer close(accepted)
				for i := 0; i < 5; i++ {
					select {
					case c.incoming <- Event{Data: payload}:
					case <-c.stop:
						return
					}
					select {
					case accepted <- i:
					case <-c.stop:
						return
					}
				}
			}()
			limit := 1
			if mib == 6 {
				limit = 3
			}
			for i := 0; i < limit; i++ {
				select {
				case <-accepted:
				case <-time.After(time.Second):
					t.Fatal("legal burst blocked too early")
				}
			}
			select {
			case <-accepted:
				t.Fatal("queue kept reading past its bounded soft limit")
			case <-time.After(30 * time.Millisecond):
			}
			for i := 0; i < 5; {
				batch, ok := readEventBatch(t, c)
				if !ok {
					t.Fatal("queue disconnected a slow consumer")
				}
				i += len(batch)
				for range batch {
					select {
					case <-accepted:
					default:
					}
				}
			}
		})
	}
}

// Mirrors the native SDK's three copies of a completed tool result. Each is
// a distinct legal JSONL frame; there is no shared broker or snapshot wrapper.
func TestLargeNativeBurstProcess(t *testing.T) {
	if os.Getenv("GOUI_LARGE_NATIVE_BURST") == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct {
			ID  string
			MiB int
		}
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			os.Exit(3)
		}
		text := strings.Repeat("x", command.MiB<<20)
		for _, prefix := range []string{
			`{"type":"tool_execution_end","toolCallId":"large","result":{"content":[{"type":"text","text":"`,
			`{"type":"message_start","message":{"role":"toolResult","toolCallId":"large","content":[{"type":"text","text":"`,
			`{"type":"message_end","message":{"role":"toolResult","toolCallId":"large","content":[{"type":"text","text":"`,
		} {
			_, _ = io.WriteString(os.Stdout, prefix)
			_, _ = io.WriteString(os.Stdout, text)
			_, _ = io.WriteString(os.Stdout, "\"}]}}\n")
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "response", "id": command.ID, "success": true, "data": map[string]bool{"done": true}})
	}
	os.Exit(0)
}
func largeNativeClient(t *testing.T) *Client {
	t.Helper()
	t.Setenv("GOUI_LARGE_NATIVE_BURST", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(Config{NoSession: true, Bin: exe, Args: []string{"-test.run=^TestLargeNativeBurstProcess$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestDirectLargeNativeBurstSurvivesSlowConsumer(t *testing.T) {
	for _, mib := range []int{6, 20} {
		t.Run(fmt.Sprint(mib), func(t *testing.T) {
			c := largeNativeClient(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			reply := make(chan error, 1)
			go func() { _, err := c.Call(ctx, map[string]any{"type": "burst", "MiB": mib}); reply <- err }()
			// A stalled view creates the formerly fatal large-result burst. Calls
			// and event consumption remain concurrent, as in the real UI.
			time.Sleep(50 * time.Millisecond)
			for _, kind := range []string{"tool_execution_end", "message_start", "message_end"} {
				select {
				case e := <-c.Events():
					if e.Err != nil {
						t.Fatal(e.Err)
					}
					var envelope struct{ Type string }
					if json.Unmarshal(e.Data, &envelope) != nil || envelope.Type != kind || len(e.Data) < mib<<20 {
						t.Fatalf("lost/reordered large native %s", kind)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err := <-reply; err != nil {
				t.Fatal(err)
			}
			if err := c.Err(); err != nil {
				t.Fatalf("slow consumer ended transport: %v", err)
			}
		})
	}
}
func TestDirectBackpressureDoesNotBlockClose(t *testing.T) {
	c := largeNativeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reply := make(chan error, 1)
	go func() { _, err := c.Call(ctx, map[string]any{"type": "burst", "MiB": 20}); reply <- err }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatal("stalled event consumer prevented shutdown")
	}
	select {
	case err := <-reply:
		if err == nil {
			t.Fatal("unfinished burst reported success")
		}
	case <-ctx.Done():
		t.Fatal("Call did not end after Close")
	}
}
