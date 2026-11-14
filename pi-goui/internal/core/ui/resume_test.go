package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	rpc "pi-goui/internal/runtime"
)

func savedSession(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history file.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session\",\"version\":3,\"id\":\"saved\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestResumeReplacesSlotAndIsolatesLateEvents(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	firstID := old.id
	old.state.File = savedSession(t)
	oldClient, _ := fakeClient(t)
	old.client = oldClient
	other := m.addTab("")
	other.setValue("other draft")
	m.activate(0)
	cfg, _ := fakeConfig(t)
	argsFile := filepath.Join(t.TempDir(), "argv")
	wrapper := filepath.Join(t.TempDir(), "capture-pi")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + strconv.Quote(argsFile) + "\nexec " + strconv.Quote(cfg.Bin) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m.opts.Bin = wrapper
	target := savedSession(t)
	before, _ := os.ReadFile(target)
	cmd := m.resume(target)
	if len(m.tabs) != 2 || m.active != 0 || m.current() == old || m.current().id == firstID || m.current().state.File != sessionPath(target) {
		t.Fatal("resume appended/reused old routing identity")
	}
	if m.tabs[1] != other || other.editor.Value() != "other draft" {
		t.Fatal("resume changed other tab")
	}
	batch, ok := cmd().(tea.BatchMsg)
	// Real terminal cursors need no separate virtual-cursor blink command.
	if !ok || len(batch) < 2 {
		t.Fatal("missing startup/cleanup commands")
	}
	start := batch[0]().(tea.BatchMsg)
	opened := start[0]().(openedMsg)
	if opened.err != nil {
		t.Fatal(opened.err)
	}
	m.Update(opened)
	batch[len(batch)-1]()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := oldClient.Call(ctx, map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("old client not closed")
	}
	deadline := time.Now().Add(time.Second)
	var argv []byte
	for time.Now().Before(deadline) {
		argv, _ = os.ReadFile(argsFile)
		if len(argv) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(string(argv), "--session\n"+sessionPath(target)+"\n") {
		t.Fatalf("wrong target startup: %q", argv)
	}
	m.Update(eventMsg{id: firstID, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_start"}`)}})
	m.Update(snapshotMsg{id: firstID, state: json.RawMessage(`{"sessionName":"wrong session"}`)})
	if m.current().state.Busy || m.current().state.Name == "wrong session" {
		t.Fatal("late old events reached replacement")
	}
	after, _ := os.ReadFile(target)
	if string(after) != string(before) {
		t.Fatal("resume modified saved file")
	}
}
func TestResumePreservesWorkAndReusesAlreadyOpenTarget(t *testing.T) {
	for _, state := range []string{"history", "loading", "draft", "busy", "compacting", "sending", "preflight", "image"} {
		t.Run(state, func(t *testing.T) {
			m := testModel(t, "")
			old := m.current()
			target := savedSession(t)
			switch state {
			case "history":
				old.state.MessageCount = 2
			case "loading":
				old.refreshing = true
			case "draft":
				old.setValue("keep me")
			case "busy":
				old.state.Busy = true
			case "compacting":
				old.state.Compacting = true
			case "sending":
				old.sending = true
			case "preflight":
				old.awaitingStart = true
			case "image":
				old.images = []map[string]any{{"data": "fixture"}}
			}
			client, _ := fakeClient(t)
			old.client = client
			if cmd := m.resume(target); cmd == nil {
				t.Fatal("missing startup command")
			}
			if len(m.tabs) != 2 || m.tabs[0] != old || m.current() == old || m.current().state.File != sessionPath(target) {
				t.Fatal("resume did not preserve source and activate a new tab")
			}
			if len(old.dialogs) != 0 || old.client != client {
				t.Fatal("resume changed source dialogs or client")
			}
			if state == "draft" && old.editor.Value() != "keep me" {
				t.Fatal("resume lost draft")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := client.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
				t.Fatal("source client is no longer usable:", err)
			}
		})
	}
	m := testModel(t, "")
	first := m.current()
	target := savedSession(t)
	existing := m.addTab(target)
	m.activate(0)
	first.setValue("keep draft")
	m.resume(target)
	if m.current() != existing || len(m.tabs) != 2 || first.editor.Value() != "keep draft" {
		t.Fatal("already-open target duplicated or discarded another draft")
	}
}
func TestResumeRejectsUnavailableAndEphemeralTargets(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	m.resume(filepath.Join(t.TempDir(), "missing"))
	if m.current() != old || len(m.tabs) != 1 {
		t.Fatal("missing target replaced tab")
	}
	m.opts.NoSession = true
	m.resume(savedSession(t))
	if m.current() != old {
		t.Fatal("--no-session silently ignored saved target")
	}
}
