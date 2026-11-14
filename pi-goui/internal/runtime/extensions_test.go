package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExtensionRPCProcess(t *testing.T) {
	mode := os.Getenv("GOUI_EXTENSION_RPC_TEST")
	if mode == "" {
		return
	}
	output := json.NewEncoder(os.Stdout)
	wire, _ := os.OpenFile(os.Getenv("GOUI_EXTENSION_RPC_WIRE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	defer wire.Close()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		_, _ = fmt.Fprintln(wire, scanner.Text())
		var command struct{ ID, Type, Message string }
		_ = json.Unmarshal(scanner.Bytes(), &command)
		data := any(map[string]any{})
		switch command.Type {
		case "get_commands":
			commands := []map[string]string{}
			if mode != "absent" {
				source := "extension"
				if mode == "template" {
					source = "prompt"
				}
				commands = append(commands, map[string]string{"name": extensionCommand, "source": source})
			}
			data = map[string]any{"commands": commands}
		case "prompt":
			if mode == "absent" || mode == "template" {
				os.Exit(8)
			}
			var request struct {
				ID, Method string
				Params     json.RawMessage
			}
			_ = json.Unmarshal([]byte(strings.TrimPrefix(command.Message, "/"+extensionCommand+" ")), &request)
			response := map[string]any{"id": request.ID, "result": request.Params}
			switch request.Method {
			case "example.error":
				response = map[string]any{"id": request.ID, "error": map[string]string{"code": "unavailable", "message": "not installed"}}
			case "example.exit":
				os.Exit(0)
			}
			if request.Method != "example.hang" {
				raw, _ := json.Marshal(response)
				_ = output.Encode(map[string]any{"type": "extension_ui_request", "method": "notify", "message": extensionReplyPrefix + string(raw)})
			}
			if request.Method == "example.notice" {
				_ = output.Encode(map[string]any{"type": "extension_ui_request", "method": "notify", "message": "ordinary notification"})
			}
			data = map[string]string{"disposition": "handled"}
		}
		_ = output.Encode(map[string]any{"type": "response", "id": command.ID, "success": true, "data": data})
	}
	os.Exit(0)
}
func extensionClient(t *testing.T, mode string) (*Client, string) {
	t.Helper()
	root := t.TempDir()
	wire := filepath.Join(root, "wire")
	t.Setenv("GOUI_EXTENSION_RPC_TEST", mode)
	t.Setenv("GOUI_EXTENSION_RPC_WIRE", wire)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(Config{Bin: executable, Args: []string{"-test.run=^TestExtensionRPCProcess$", "--"}, Cwd: root, AgentDir: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, wire
}
func TestExtensionRPCDiscoveryNeverPromptsWithoutBridge(t *testing.T) {
	for _, mode := range []string{"absent", "template"} {
		t.Run(mode, func(t *testing.T) {
			c, wire := extensionClient(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := c.CallExtension(ctx, "example.echo", nil)
			if !errors.Is(err, ErrExtensionUnavailable) {
				t.Fatalf("err=%v", err)
			}
			raw, _ := os.ReadFile(wire)
			if strings.Contains(string(raw), `"type":"prompt"`) {
				t.Fatal("sent unknown command")
			}
		})
	}
}
func TestExtensionRPCCorrelatesConcurrentRepliesAndPreservesNotifications(t *testing.T) {
	c, _ := extensionClient(t, "present")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raw, err := c.CallExtension(ctx, "example.echo", i)
			if err != nil || string(raw) != fmt.Sprint(i) {
				t.Errorf("reply %d: %s %v", i, raw, err)
			}
		}(i)
	}
	wg.Wait()
	_, err := c.CallExtension(ctx, "example.error", nil)
	if !errors.Is(err, ErrExtensionUnavailable) {
		t.Fatalf("err=%v", err)
	}
	_, err = c.CallExtension(ctx, "example.notice", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-c.Events():
		if !strings.Contains(string(event.Data), "ordinary notification") {
			t.Fatalf("event=%s", event.Data)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	c.mu.Lock()
	count := len(c.extensionPending)
	c.mu.Unlock()
	if count != 0 {
		t.Fatal("pending calls leaked")
	}
}
func TestExtensionRPCCancellationAndExitCleanPending(t *testing.T) {
	for _, method := range []string{"example.hang", "example.exit"} {
		t.Run(method, func(t *testing.T) {
			c, _ := extensionClient(t, "present")
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := c.CallExtension(ctx, method, nil)
			if err == nil {
				t.Fatal("expected failure")
			}
			c.mu.Lock()
			count := len(c.extensionPending)
			c.mu.Unlock()
			if count != 0 {
				t.Fatal("pending calls leaked")
			}
		})
	}
}
func TestExtensionReplyDoesNotConsumeUnrelatedMessages(t *testing.T) {
	c := &Client{}
	for _, raw := range []string{
		`{"type":"extension_ui_request","method":"notify","message":"ordinary"}`,
		`{"type":"extension_ui_request","method":"notify","message":"pi-ui:rpc:v1:{broken"}`,
		`{"type":"extension_ui_request","method":"notify","message":"pi-ui:rpc:v1:{\"id\":\"another-client\",\"result\":0}"}`,
	} {
		if c.extensionReply([]byte(raw)) {
			t.Fatalf("consumed %s", raw)
		}
	}
	// Our late cancelled response must not become a user-visible JSON notice.
	if !c.extensionReply([]byte(`{"type":"extension_ui_request","method":"notify","message":"pi-ui:rpc:v1:{\"id\":\"goui-ext-1\",\"result\":0}"}`)) {
		t.Fatal("late response leaked")
	}
}
