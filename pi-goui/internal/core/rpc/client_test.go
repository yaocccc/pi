package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRPCChild(t *testing.T) {
	if os.Getenv("GOUI_RPC_TEST_CHILD") == "" {
		return
	}
	if os.Getenv("GOUI_RPC_TEST_CHILD") == "blocked" {
		for {
			time.Sleep(time.Hour)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	var mu sync.Mutex
	var wg sync.WaitGroup
	emit := func(v any) { mu.Lock(); defer mu.Unlock(); _ = json.NewEncoder(os.Stdout).Encode(v) }
	for scanner.Scan() {
		var c map[string]any
		if json.Unmarshal(scanner.Bytes(), &c) != nil {
			os.Exit(3)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch c["type"] {
			case "flood":
				for i := 0; i < 700; i++ {
					emit(map[string]any{"type": "future_event", "index": i, "text": "你好"})
				}
			case "failure":
				emit(map[string]any{"id": c["id"], "type": "response", "command": "failure", "success": false, "error": "expected failure"})
				return
			case "env":
				c["value"] = map[string]string{"agentDir": os.Getenv("PI_CODING_AGENT_DIR"), "session": os.Getenv("PI_SESSION_ID"), "kitty": os.Getenv("KITTY_WINDOW_ID"), "config": os.Getenv("GOUI_KEEP"), "protocol": os.Getenv("PI_GOUI_PROTOCOL"), "uiProtocol": os.Getenv("PI_UI_PROTOCOL")}
			}
			emit(map[string]any{"type": "response", "id": c["id"], "success": true, "data": map[string]any{"value": c["value"]}})
			if os.Getenv("GOUI_RPC_TEST_CHILD") == "once" {
				os.Exit(0)
			}
		}()
	}
	wg.Wait()
	os.Exit(0)
}
func testClient(t *testing.T, mode string) *Client {
	t.Helper()
	t.Setenv("GOUI_RPC_TEST_CHILD", mode)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(Config{Bin: bin, Args: []string{"-test.run=^TestRPCChild$", "--"}, AgentDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestConcurrentCallsAndQueuedEvents(t *testing.T) {
	c := testClient(t, "normal")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, err := c.Call(ctx, map[string]any{"type": "echo", "value": i})
			if err != nil {
				t.Error(err)
				return
			}
			var v struct{ Value int }
			if json.Unmarshal(raw, &v) != nil || v.Value != i {
				t.Errorf("wrong response: %s for %d", raw, i)
			}
		}(i)
	}
	wg.Wait()
	if _, err := c.Call(ctx, map[string]any{"type": "flood"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 700; i++ {
		select {
		case e := <-c.Events():
			if e.Err != nil {
				t.Fatal(e.Err)
			}
			var v struct {
				Index int
				Text  string
			}
			json.Unmarshal(e.Data, &v)
			if v.Index != i || v.Text != "你好" {
				t.Fatalf("bad event %s", e.Data)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if _, err := c.Call(ctx, map[string]any{"type": "failure"}); err == nil || !strings.Contains(err.Error(), "expected failure") {
		t.Fatalf("missing RPC error: %v", err)
	}
}
func TestEnvironmentIsolation(t *testing.T) {
	t.Setenv("PI_SESSION_ID", "parent")
	t.Setenv("KITTY_WINDOW_ID", "parent-pane")
	t.Setenv("GOUI_KEEP", "preserved")
	t.Setenv("PI_GOUI_PROTOCOL", "stale")
	t.Setenv("PI_UI_PROTOCOL", "stale")
	c := testClient(t, "normal")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, map[string]any{"type": "env"})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Value map[string]string }
	json.Unmarshal(raw, &v)
	if v.Value["session"] != "" || v.Value["kitty"] != "" || v.Value["config"] != "preserved" || v.Value["agentDir"] == "" || v.Value["protocol"] != "" || v.Value["uiProtocol"] != "" {
		t.Fatalf("environment: %s", raw)
	}
}
func TestDeadlineAndConcurrentClose(t *testing.T) {
	c := testClient(t, "blocked")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Call(ctx, map[string]any{"type": "echo", "value": strings.Repeat("x", 4<<20)})
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("deadline: %v %s", err, time.Since(start))
	}
	start = time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if time.Since(start) > 4*time.Second {
		t.Fatal("unbounded close")
	}
	select {
	case <-c.done:
	default:
		t.Fatal("child survived close")
	}
}
func TestFinalResponseBeforeExit(t *testing.T) {
	c := testClient(t, "once")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := c.Call(ctx, map[string]any{"type": "echo", "value": "last"})
	if err != nil || !strings.Contains(string(raw), "last") {
		t.Fatalf("lost final response: %s %v", raw, err)
	}
	<-c.done
	if err := c.Send(context.Background(), map[string]any{"type": "echo"}); err == nil {
		t.Fatal("send to exited child succeeded")
	}
}

func TestInvalidExplicitExecutable(t *testing.T) {
	t.Setenv("PI_BIN", fmt.Sprintf("%s/missing", t.TempDir()))
	if _, err := Start(Config{}); err == nil {
		t.Fatal("invalid explicit PI_BIN silently ignored")
	}
}
