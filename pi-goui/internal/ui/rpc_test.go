package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/rpc"
)

// Executed only by a temporary wrapper pointed at this test binary, never pi.
func TestUIRPCProcess(t *testing.T) {
	if os.Getenv("PIGOUI_UI_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req map[string]any
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		kind, _ := req["type"].(string)
		if path := os.Getenv("PIGOUI_UI_WIRE"); path != "" {
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
			if err == nil {
				b, _ := json.Marshal(req)
				_, _ = f.Write(append(b, '\n'))
				_ = f.Close()
			}
		}
		if kind == "extension_ui_response" {
			continue
		}
		data := any(map[string]any{})
		switch kind {
		case "get_state":
			data = map[string]any{"sessionId": "fake", "thinkingLevel": "medium", "model": map[string]any{"id": "one", "provider": "fake"}}
		case "get_entries":
			data = map[string]any{"entries": []any{}, "leafId": nil}
		case "get_commands":
			data = map[string]any{"commands": []any{}}
		case "get_available_models":
			data = map[string]any{"models": []any{map[string]any{"id": "one", "provider": "fake"}, map[string]any{"id": "two", "provider": "fake"}}}
		case "prompt":
			data = map[string]any{"disposition": "accepted"}
		}
		response := map[string]any{"type": "response", "id": req["id"], "command": kind, "success": true, "data": data}
		if kind == "prompt" && strings.TrimSpace(fmt.Sprint(req["message"])) == "fail" {
			response["success"] = false
			response["error"] = "fake rejection"
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}
func fakeConfig(t *testing.T) (rpc.Config, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX wrapper")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pi")
	wire := filepath.Join(dir, "wire.jsonl")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nPIGOUI_UI_HELPER=1 PIGOUI_UI_WIRE=" + quote(wire) + " exec " + quote(exe) + " -test.run=^TestUIRPCProcess$ -- \"$@\"\n"
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return rpc.Config{Bin: bin, Cwd: dir, AgentDir: dir, NoSession: true}, wire
}
func fakeClient(t *testing.T) (*rpc.Client, string) {
	t.Helper()
	cfg, wire := fakeConfig(t)
	c, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, wire
}
func wireRecords(t *testing.T, path string, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(path)
		var out []map[string]any
		for _, line := range strings.Split(string(raw), "\n") {
			var v map[string]any
			if json.Unmarshal([]byte(line), &v) == nil {
				out = append(out, v)
			}
		}
		if len(out) >= n {
			return out
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("wire %s did not reach %d records", path, n)
	return nil
}

// Execute a command/batch once. No Update loop is run, so timers cannot recurse.
func commandResult(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if result := commandResult(child); result != nil {
				return result
			}
		}
		return nil
	}
	return msg
}
func TestEnterActuallySubmitsAndFailurePreservesImages(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	c, wire := fakeClient(t)
	tab.client = c
	tab.setValue("hello")
	m.search = "old search"
	cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter swallowed by searchNext binding")
	}
	reply, ok := cmd().(replyMsg)
	if !ok {
		t.Fatal("Enter did not issue prompt RPC")
	}
	m.Update(reply)
	records := wireRecords(t, wire, 1)
	if records[0]["type"] != "prompt" || records[0]["message"] != "hello" {
		t.Fatalf("wrong native submit: %v", records)
	}
	if len(tab.history) != 1 {
		t.Fatal("accepted prompt not in editor history")
	}
	tab.setValue(" fail ")
	tab.images = []map[string]any{{"type": "image", "data": "AA==", "mimeType": "image/png"}}
	cmd = m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	reply = cmd().(replyMsg)
	if reply.err == nil {
		t.Fatal("fake rejection missing")
	}
	m.Update(reply)
	assertValue(t, tab, " fail ")
	if len(tab.images) != 1 || len(tab.history) != 1 {
		t.Fatal("failed prompt lost images or duplicated history")
	}
}
func TestDialogResponsesStayWithOriginatingClient(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.client, _ = fakeClient(t)
	second := m.addTab("")
	var wire string
	second.client, wire = fakeClient(t)
	second.setValue("second draft")
	raw := json.RawMessage(`{"type":"extension_ui_request","id":"original-client","method":"confirm","title":"Allow?"}`)
	m.extension(second, raw)
	m.activate(0)
	// Expiring an inactive dialog must answer its own client and leave foreground alone.
	second.dialogs[0].deadline = time.Now().Add(-time.Second)
	_, cmd := m.Update(tickMsg(time.Now()))
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("expected timeout response + next tick")
	}
	if len(batch) == 0 {
		t.Fatal("no response command")
	}
	_ = batch[0]()
	records := wireRecords(t, wire, 1)
	if records[0]["id"] != "original-client" || records[0]["cancelled"] != true {
		t.Fatalf("misrouted timeout: %v", records)
	}
	if len(second.dialogs) != 0 || m.current() != first {
		t.Fatal("timeout changed focus or left dialog")
	}
	assertValue(t, second, "second draft")
	m.extension(second, json.RawMessage(`{"id":"yes-id","method":"confirm","title":"Confirm"}`))
	m.activate(1)
	second.dialogs[0].selected = 1
	_ = commandResult(m.confirmDialog(second, second.dialogs[0]))
	records = wireRecords(t, wire, 2)
	if records[1]["id"] != "yes-id" || records[1]["confirmed"] != true {
		t.Fatalf("confirm schema: %v", records[1])
	}
	m.extension(second, json.RawMessage(`{"id":"editor-id","method":"editor","title":"Edit","prefill":"base"}`))
	press(m, tea.KeyEnter, tea.ModShift, "")
	press(m, 'x', 0, "x")
	_ = commandResult(m.key(tea.KeyPressMsg{Code: tea.KeyEnter}))
	records = wireRecords(t, wire, 3)
	if records[2]["id"] != "editor-id" || records[2]["value"] != "base\nx" {
		t.Fatalf("editor confirmation: %v", records[2])
	}
	assertValue(t, second, "second draft")
}
func TestNativeModelKeysReachCorrectRPC(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	c, wire := fakeClient(t)
	tab.client = c
	tab.state.Model.Provider = "fake"
	tab.state.Model.ID = "one"
	reply := m.key(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})().(replyMsg)
	if reply.op != "get_available_models" {
		t.Fatal("Ctrl+L not model select")
	}
	m.Update(reply)
	if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "model" {
		t.Fatal("model picker not opened")
	}
	tab.dialogs = nil
	reply = m.key(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift})().(replyMsg)
	if reply.op != "set_model" || reply.err != nil {
		t.Fatalf("reverse cycle failed: %+v", reply)
	}
	records := wireRecords(t, wire, 3)
	last := records[len(records)-1]
	if last["type"] != "set_model" || last["modelId"] != "two" {
		t.Fatalf("reverse cycle wire: %v", last)
	}
}
func TestClientPoolCloseRacesStartup(t *testing.T) {
	cfg, _ := fakeConfig(t)
	pool := &clientPool{clients: map[*rpc.Client]bool{}}
	if _, err := pool.start(cfg); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, _ = pool.start(cfg) }()
	}
	close(start)
	done := make(chan struct{})
	go func() { _ = pool.close(); close(done) }()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("close blocked on concurrent startup")
	}
	if _, err := pool.start(cfg); err == nil {
		t.Fatal("closed pool accepted process")
	}
	pool.mu.Lock()
	clients := make([]*rpc.Client, 0, len(pool.clients))
	for c := range pool.clients {
		clients = append(clients, c)
	}
	pool.mu.Unlock()
	for _, c := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := c.Call(ctx, map[string]any{"type": "get_state"})
		cancel()
		if err == nil {
			t.Fatal("pool left child running")
		}
	}
}
