package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"pi-goui/internal/core/config"
	rpc "pi-goui/internal/runtime"
)

// A native-RPC fixture, never a real Pi/model. The normal shared helper path in
// TestMain composes the production capability observer in the executor process.
func TestWorkingSharedPiHelper(t *testing.T) {
	if os.Getenv("GOUI_WORKING_PI_HELPER") != "1" {
		return
	}
	emit := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	prompts := 0
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var cmd map[string]any
		if json.Unmarshal(scanner.Bytes(), &cmd) != nil {
			os.Exit(2)
		}
		var data any = map[string]any{}
		switch cmd["type"] {
		case "get_state":
			data = map[string]any{"sessionFile": os.Getenv("GOUI_WORKING_FILE"), "sessionId": "working-fixture", "isStreaming": prompts > 0}
		case "get_entries":
			data = map[string]any{"entries": []any{}}
		case "get_commands":
			data = map[string]any{"commands": []any{}}
		case "get_fixture_count":
			data = map[string]any{"prompts": prompts}
		case "prompt":
			prompts++
			emit(map[string]any{"type": "agent_start"})
			for i, output := range []int{200, 300, 150} {
				emit(map[string]any{"type": "turn_start", "turnIndex": i})
				kind := "message_end"
				if i == 2 {
					kind = "message_update"
				}
				emit(map[string]any{"type": kind, "message": map[string]any{"role": "assistant", "timestamp": i + 1, "content": []any{map[string]any{"type": "text", "text": "body"}}, "usage": map[string]any{"input": 50000, "output": output}}})
			}
			emit(json.RawMessage(`{"type":"tool_execution_update","toolCallId":"worker-1","partialResult":{"details":{"kind":"worker-ui","batchId":"fixture","tasks":[{"index":0,"usage":{"input":1000,"output":15}}]}}}`))
		}
		emit(map[string]any{"type": "response", "id": cmd["id"], "command": cmd["type"], "success": true, "data": data})
	}
	os.Exit(0)
}

func TestWorkingSharedTransportIncludesConfiguredCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shared executor")
	}
	if os.Getenv("GOUI_SHARED_WORKING_TEST") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestWorkingSharedTransportIncludesConfiguredCapabilities$", "-test.count=1")
		cmd.Env = append(os.Environ(), "GOUI_SHARED_WORKING_TEST=1", "HOME="+root, "PI_CODING_AGENT_DIR="+filepath.Join(root, "agent"), "PI_OFFLINE=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated shared working subprocess: %v\n%s", err, output)
		}
		return
	}
	rpc.EnableSessionSharing()
	root := t.TempDir()
	file := filepath.Join(root, "session.jsonl")
	if err := os.WriteFile(file, []byte("{\"type\":\"session\",\"id\":\"working-fixture\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOUI_WORKING_PI_HELPER", "1")
	t.Setenv("GOUI_WORKING_FILE", file)
	exe, _ := os.Executable()
	cfg := rpc.Config{Bin: exe, Cwd: root, AgentDir: filepath.Join(root, "agent"), SessionFile: file, Args: []string{"-test.run=^TestWorkingSharedPiHelper$", "--"}}
	member, err := rpc.JoinSessionMember(cfg.AgentDir)
	if err != nil {
		t.Fatal(err)
	}
	defer member.Close()
	first, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := first.Call(ctx, map[string]any{"type": "prompt", "message": "fixture only"}); err != nil {
		t.Fatal(err)
	}
	original := testModel(t, "{}")
	original.current().client = first
	deliverUntil := func(m *Model, client *rpc.Client, kind string) {
		t.Helper()
		for {
			select {
			case event, ok := <-client.Events():
				if !ok || event.Err != nil {
					t.Fatalf("shared transport closed: %v", event.Err)
				}
				var envelope struct{ Type string }
				_ = json.Unmarshal(event.Data, &envelope)
				m.Update(eventMsg{id: m.current().id, client: client, event: event})
				if envelope.Type == kind {
					return
				}
			case <-ctx.Done():
				t.Fatal("timed out waiting for " + kind)
			}
		}
	}
	deliverUntil(original, first, "tool_execution_update")
	second, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	late := testModel(t, "{}")
	late.current().client = second
	late.current().editor.SetValue("late local draft")
	revision := late.current().revision
	deliverUntil(late, second, "goui_working_snapshot")
	if late.current().revision != revision {
		t.Fatal("working bootstrap invalidated transcript preparation")
	}
	clock := original.current().working.Started.Add(27 * time.Second)
	want := "3 turns · ↑51k ↓665 · 24.6 TPS · 27s"
	for name, m := range map[string]*Model{"original": original, "late": late} {
		if got := m.current().working.line(clock, config.Theme{}); got != want {
			t.Fatalf("%s: %q, want %q", name, got, want)
		}
	}
	if !late.current().working.Started.Equal(original.current().working.Started) || first.PID() != second.PID() || late.current().editor.Value() != "late local draft" {
		t.Fatal("clock/backend/draft isolation changed")
	}
	data, err := second.Call(ctx, map[string]any{"type": "get_fixture_count"})
	if err != nil || string(data) != `{"prompts":1}` {
		t.Fatalf("bootstrap replayed a prompt: %s %v", data, err)
	}
}
