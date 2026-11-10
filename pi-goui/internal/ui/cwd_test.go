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
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/rpc"
	"pi-goui/internal/session"
)

func TestResolveCwd(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "目录 with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	for _, value := range []string{dir, "目录 with spaces", "~/目录 with spaces", "./目录 with spaces/../目录 with spaces"} {
		got, err := resolveCwd(value, root)
		if err != nil || got != dir {
			t.Fatalf("resolve %q = %q, %v", value, got, err)
		}
	}
	if got, err := resolveCwd("~", dir); err != nil || got != root {
		t.Fatalf("home = %q, %v", got, err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "  ", "missing", file} {
		if _, err := resolveCwd(value, root); err == nil {
			t.Fatalf("accepted invalid directory %q", value)
		}
	}
}

func TestCwdSidebarClickAndEligibility(t *testing.T) {
	for _, blocked := range []string{"", "history", "items", "messages", "busy", "compacting", "sending", "preflight", "queued", "image-pending", "clipboard", "modal", "mutation"} {
		t.Run(blocked, func(t *testing.T) {
			m := testModel(t, "")
			m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			tab := m.current()
			switch blocked {
			case "history":
				tab = m.addTab(savedSession(t))
			case "items":
				tab.state.Items = []session.Item{{ID: "1", Role: "user", Text: "hi"}}
			case "messages":
				tab.state.MessageCount = 1
			case "busy":
				tab.state.Busy = true
			case "compacting":
				tab.state.Compacting = true
			case "sending":
				tab.sending = true
			case "preflight":
				tab.awaitingStart = true
			case "queued":
				tab.state.FollowUp = []string{"next"}
			case "image-pending":
				tab.imagePending = 1
			case "clipboard":
				m.clipboardTarget = tab
			case "modal":
				tab.dialogs = []*dialog{m.newDialog("help", "Help", "")}
			case "mutation":
				tab.mutation = &sessionAction{}
			}
			layout := m.sidebarLayout()
			rows := strings.Split(ansi.Strip(render.Sidebar(tab.state, nil, m.cfg.Theme, m.sidebarWidth(), m.height, m.sidebarContext())), "\n")
			if layout.DirectoryRow != 3 || !strings.Contains(rows[layout.DirectoryRow], "/") {
				t.Fatal("directory hit geometry no longer matches rendering")
			}
			m.mouse(tea.Mouse{X: m.mainWidth() + 3, Y: layout.DirectoryRow, Button: tea.MouseLeft})
			if blocked != "" {
				for _, d := range tab.dialogs {
					if d.kind == "cwd" {
						t.Fatal("nonempty/busy/saved session opened cwd dialog")
					}
				}
				return
			}
			if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "cwd" || filepath.Clean(tab.dialogs[0].editor.Value()) != tab.state.Cwd || !popupFilter(tab.dialogs[0]) {
				t.Fatal("directory click did not open prefilled input")
			}
			press(m, tea.KeyEscape, 0, "")
			if len(tab.dialogs) != 0 || m.current() != tab {
				t.Fatal("cancel replaced the session")
			}
		})
	}
}

func TestCwdValidationAndRecheck(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	m.openCwd(old)
	d := old.dialogs[0]
	d.editor.SetValue(filepath.Join(t.TempDir(), "missing"))
	m.confirmDialog(old, d)
	if m.current() != old || len(old.dialogs) != 1 || !strings.Contains(d.message, "missing") {
		t.Fatal("invalid directory changed session or lost the input dialog")
	}
	d.editor.SetValue(t.TempDir())
	old.state.MessageCount = 1
	m.confirmDialog(old, d)
	if m.current() != old || !strings.Contains(d.message, "empty") {
		t.Fatal("confirmation did not recheck emptiness")
	}
	old.state.MessageCount = 0
	d.editor.SetValue(old.state.Cwd)
	m.confirmDialog(old, d)
	if m.current() != old || len(old.dialogs) != 0 {
		t.Fatal("unchanged cwd restarted the backend")
	}
}

func TestCwdReplacementStartsInDirectoryAndIsolatesSession(t *testing.T) {
	m := testModel(t, "")
	old := m.current()
	base := m.opts.Cwd
	oldClient, _ := fakeClient(t)
	old.client = oldClient
	old.setValue("draft [image1]")
	old.images = []map[string]any{{"data": "keep", "mimeType": "image/png"}}
	other := m.addTab("")
	other.setValue("other draft")
	m.activate(0)
	dir := t.TempDir()
	cfg, _ := fakeConfig(t)
	capture := filepath.Join(t.TempDir(), "cwd")
	wrapper := filepath.Join(t.TempDir(), "capture-pi")
	script := "#!/bin/sh\npwd > " + strconv.Quote(capture) + "\nexec " + strconv.Quote(cfg.Bin) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m.opts.Bin = wrapper
	m.openCwd(old)
	d := old.dialogs[0]
	d.editor.SetValue(dir)
	cmd := m.confirmDialog(old, d)
	replacement := m.current()
	if replacement == old || replacement.id == old.id || len(m.tabs) != 2 || m.active != 0 || replacement.state.Cwd != dir || replacement.state.File != "" || !replacement.newSession {
		t.Fatal("cwd did not replace the empty session in place")
	}
	if replacement.editor.Value() != "draft [image1]" || len(replacement.images) != 1 || len(replacement.dialogs) != 0 {
		t.Fatal("replacement lost unsent input or retained modal")
	}
	if m.opts.Cwd != base || m.tabs[1] != other || other.state.Cwd != base || other.editor.Value() != "other draft" || m.sidebarContext().Directory != dir {
		t.Fatal("cwd leaked into global options or another tab")
	}
	// Run only the commands returned by confirmation, not the commands from
	// Update (which would start the event listener and background refresh loop).
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, child := range msg {
				run(child)
			}
		case openedMsg:
			if msg.err != nil {
				t.Fatal(msg.err)
			}
			m.Update(msg)
		}
	}
	run(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := replacement.client.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(capture)
	if err != nil || strings.TrimSpace(string(actual)) != dir {
		t.Fatalf("backend cwd = %q, %v", actual, err)
	}
	if _, err := oldClient.Call(ctx, map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("old backend remained running")
	}
	m.Update(eventMsg{id: old.id, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_start"}`)}})
	m.Update(snapshotMsg{id: old.id, state: json.RawMessage(`{"sessionName":"stale"}`)})
	if replacement.state.Busy || replacement.state.Name == "stale" || replacement.state.Cwd != dir {
		t.Fatal("late old events contaminated replacement")
	}
}

func TestCwdFileCompletionUsesActiveDirectory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cwd-only.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	tab.state.Cwd = dir
	tab.setValue("@cwd")
	cmd := m.ensureFileCompletion()
	if cmd == nil {
		t.Fatal("no file lookup")
	}
	m.receiveFiles(cmd().(filesMsg))
	choices := m.completion(tab)
	if len(choices) != 1 || choices[0].Name != "cwd-only.txt" {
		t.Fatalf("completion did not use tab cwd: %+v", choices)
	}
	m.addTab("").setValue("@cwd")
	if choices := m.completion(m.current()); len(choices) != 0 {
		t.Fatalf("other tab reused cwd-specific results: %+v", choices)
	}
}
