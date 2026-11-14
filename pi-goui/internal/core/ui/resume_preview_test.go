package ui

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func previewHistory(t *testing.T) string {
	t.Helper()
	file := savedSession(t)
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString(`{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"SAVED-LOCAL-HISTORY"}]}}` + "\n")
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func previewModel(t *testing.T) (*Model, *tab) {
	t.Helper()
	m := testModel(t, "")
	m.replaceSession(m.current(), previewHistory(t))
	return m, m.current()
}

func TestResumePreviewBeforeConnectionAndPreservesDraft(t *testing.T) {
	m, tab := previewModel(t)
	before, _ := os.ReadFile(tab.state.File)
	preview := m.previewSession(tab)()
	tab.setValue("typing while loading [image1]")
	tab.images = []map[string]any{{"type": "image", "data": "local"}}
	revision := tab.draftRevision
	m.Update(preview)
	if tab.client != nil || !tab.resume.preview || len(tab.state.Items) != 1 || tab.state.Items[0].Text != "SAVED-LOCAL-HISTORY" {
		t.Fatal("saved history did not appear before the connection")
	}
	assertValue(t, tab, "typing while loading [image1]")
	if tab.draftRevision != revision || len(tab.images) != 1 || tab.images[0]["data"] != "local" {
		t.Fatal("preview changed local draft ownership")
	}
	if tab.documentJob == nil {
		t.Fatal("preview did not schedule its layout off the event loop")
	}
	m.Update(tab.documentJob.render())
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "SAVED-LOCAL-HISTORY") || strings.Contains(strings.ToLower(view), "starting") || strings.Contains(strings.ToLower(view), "stopped") {
		t.Fatalf("misleading or absent preview: %s", view)
	}
	after, _ := os.ReadFile(tab.state.File)
	if string(before) != string(after) {
		t.Fatal("preview changed native history")
	}
}

func TestResumePreviewRejectsStaleOwnerPathRevisionAndAuthority(t *testing.T) {
	for _, kind := range []string{"owner", "path", "revision", "authority"} {
		t.Run(kind, func(t *testing.T) {
			m, tab := previewModel(t)
			preview := m.previewSession(tab)()
			switch kind {
			case "owner":
				m.replaceSession(tab, savedSession(t))
			case "path":
				tab.state.File = savedSession(t)
			case "revision":
				tab.revision++
			case "authority":
				tab.resume.authoritative = true
			}
			m.Update(preview)
			if len(m.current().state.Items) != 0 {
				t.Fatal("stale disk projection replaced current state")
			}
		})
	}
}

func TestResumePreviewFailureRetainsHistoryAndUnsentInputs(t *testing.T) {
	m, tab := previewModel(t)
	preview := m.previewSession(tab)()
	tab.setValue("first [image1]")
	tab.images = []map[string]any{{"data": "first"}}
	m.submitChecked(tab, false, true)
	tab.setValue("later [image1]")
	tab.images = []map[string]any{{"data": "later"}}
	m.Update(openedMsg{id: tab.id, err: errors.New("test connection refused")})
	// A fast transport failure must not prevent a slower disk preview arriving.
	m.Update(preview)
	assertValue(t, tab, "first [image1]\n\nlater [image2]")
	if len(tab.images) != 2 || tab.resume.submit != nil || len(tab.state.Items) != 1 {
		t.Fatal("connection failure lost history, attachments or input")
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "连接") || strings.Contains(strings.ToLower(view), "stopped") || !strings.Contains(m.status(tab), "test connection refused") {
		t.Fatalf("failure was not accurate: %s", view)
	}
	if cmd := m.submitChecked(tab, false, true); cmd != nil {
		t.Fatal("failed connection retried a submission")
	}
}

func TestResumeEarlyKeystrokeIsNotDiscarded(t *testing.T) {
	m, tab := previewModel(t)
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	assertValue(t, tab, "x")
	// A known invalid history still gates editing as before.
	tab.sessionGuard = sessionGuard{path: tab.state.File, warning: uncertainWarning, checked: time.Now()}
	m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	assertValue(t, tab, "x")
	if len(tab.dialogs) == 0 {
		t.Fatal("known warning was bypassed")
	}
}

func TestResumeDeferredUnknownResultKeepsBothDraftsWithoutReplay(t *testing.T) {
	m, tab := previewModel(t)
	tab.resume.pending = false
	tab.setValue("later [image1]")
	tab.images = []map[string]any{{"data": "later"}}
	cmd := m.reply(tab, replyMsg{op: "prompt", deferred: true, text: "first [image1]", images: []map[string]any{{"data": "first"}}, err: errors.New("unknown completion")})
	if cmd != nil || tab.resume.submit != nil {
		t.Fatal("unknown operation was automatically retried")
	}
	assertValue(t, tab, "first [image1]\n\nlater [image2]")
	if len(tab.images) != 2 || !strings.Contains(tab.status, "unknown completion") {
		t.Fatal("recovery lost attachments or hid uncertainty")
	}
}

func TestResumeDeferredSubmitRechecksHistoryAfterSlowConnect(t *testing.T) {
	m, tab := previewModel(t)
	tab.setValue("held")
	m.submitChecked(tab, false, true)
	pending := tab.resume.submit
	if err := os.WriteFile(tab.state.File, []byte("broken history\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Simulate the read-only check returned by flushResumeSubmission. A
	// warning must restore the original intent, never send the newer editor.
	tab.resume.pending = false
	tab.setValue("later")
	m.Update(resumeSubmitReadyMsg{owner: tab, file: tab.state.File, pending: pending, err: errors.New("invalid history")})
	assertValue(t, tab, "held\n\nlater")
	if len(tab.dialogs) == 0 || tab.dialogs[0].kind != "session-send-confirm" || tab.resume.submit != nil {
		t.Fatal("late history failure bypassed the normal confirmation guard")
	}
}

func TestResumePreviewReadFailureIsNotSuccessfulHistory(t *testing.T) {
	m, tab := previewModel(t)
	if err := os.WriteFile(tab.state.File, []byte("not json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.Update(m.previewSession(tab)())
	if tab.resume.preview || !strings.Contains(tab.status, "无法读取") || !tab.resume.pending {
		t.Fatal("invalid history presented as successful or blocked RPC recovery")
	}
}

func TestResumeDeferredInputCancelAndClose(t *testing.T) {
	m, tab := previewModel(t)
	tab.setValue("first")
	m.submitChecked(tab, false, true)
	if tab.resume.submit == nil || tab.editor.Value() != "" || tab.sending {
		t.Fatal("input was not kept as local intent")
	}
	m.requestCloseSession(tab, false)
	if len(tab.dialogs) == 0 {
		t.Fatal("closing silently discarded pending intent")
	}
	tab.dialogs = nil
	tab.setValue("later")
	m.interrupt(tab)
	assertValue(t, tab, "first\n\nlater")
	if tab.resume.submit != nil {
		t.Fatal("cancel left a future submission armed")
	}
}

// Delays native get_state, not a production hook or a model request.
func TestResumePreviewRPCProcess(t *testing.T) {
	if os.Getenv("PIGOUI_UI_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req map[string]any
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		f, err := os.OpenFile(os.Getenv("PIGOUI_UI_WIRE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			os.Exit(2)
		}
		_ = json.NewEncoder(f).Encode(req)
		_ = f.Close()
		data := any(map[string]any{})
		switch req["type"] {
		case "get_state":
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(os.Getenv("GOUI_RESUME_RELEASE")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(3)
				}
				time.Sleep(5 * time.Millisecond)
			}
			data = map[string]any{"sessionId": "saved", "sessionFile": os.Getenv("GOUI_RESUME_FILE"), "isStreaming": os.Getenv("GOUI_RESUME_BUSY") == "1"}
		case "get_entries":
			data = json.RawMessage(`{"entries":[{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"SAVED-LOCAL-HISTORY"}]}}],"leafId":"u1"}`)
		case "prompt":
			data = map[string]any{"disposition": "accepted"}
		}
		if encoder.Encode(map[string]any{"type": "response", "id": req["id"], "command": req["type"], "success": true, "data": data}) != nil {
			os.Exit(2)
		}
	}
	os.Exit(0)
}

func TestResumeDeferredInputWaitsForAuthorityAndSendsExactlyOnce(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			m, tab := previewModel(t)
			cfg, wire := fakeConfig(t)
			wrapper, err := os.ReadFile(cfg.Bin)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cfg.Bin, []byte(strings.Replace(string(wrapper), "TestUIRPCProcess", "TestResumePreviewRPCProcess", 1)), 0700); err != nil {
				t.Fatal(err)
			}
			release := filepath.Join(t.TempDir(), "release")
			t.Setenv("GOUI_RESUME_RELEASE", release)
			t.Setenv("GOUI_RESUME_FILE", tab.state.File)
			if busy {
				t.Setenv("GOUI_RESUME_BUSY", "1")
			}
			m.opts.Bin = cfg.Bin
			start := m.start(tab)().(tea.BatchMsg)
			m.Update(start[1]()) // The read-only preview is independent of start.
			opened := start[0]().(openedMsg)
			if opened.err != nil {
				t.Fatal(opened.err)
			}
			m.Update(opened)
			// openedMsg schedules refresh; run that command separately, not its
			// event reader, so the initial response stays deliberately blocked.
			tab.refreshing = false
			refresh := m.refresh(tab)
			snapshots := make(chan tea.Msg, 1)
			go func() { snapshots <- refresh() }()
			wireRecords(t, wire, 1)
			if !tab.resume.preview || len(tab.state.Items) != 1 {
				t.Fatal("history absent while get_state waits")
			}
			tab.setValue("captured [image1]")
			tab.images = []map[string]any{{"type": "image", "data": "YQ==", "mimeType": "image/png"}}
			m.submitChecked(tab, false, false)
			tab.setValue("later draft")
			other := m.addTab("")
			other.setValue("other tab")
			if records := wireRecords(t, wire, 1); len(records) != 1 || records[0]["type"] != "get_state" {
				t.Fatalf("sent before authority: %v", records)
			}
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			var snapshot tea.Msg
			select {
			case snapshot = <-snapshots:
			case <-time.After(3 * time.Second):
				t.Fatal("initial refresh did not finish")
			}
			_, send := m.Update(snapshot)
			result := commandResult(send)
			if ready, ok := result.(resumeSubmitReadyMsg); ok {
				_, send = m.Update(ready)
				result = commandResult(send)
			}
			reply, ok := result.(replyMsg)
			if !ok || reply.err != nil {
				t.Fatalf("no successful deferred submission: %#v", reply)
			}
			m.Update(reply)
			m.Update(snapshot) // A duplicate cannot submit again.
			assertValue(t, tab, "later draft")
			assertValue(t, other, "other tab")
			// Body load now needs only state + entries; optional metadata is
			// independently scheduled and does not delay this captured prompt.
			n := 3
			if busy {
				n++
			}
			records := wireRecords(t, wire, n)
			prompts := 0
			for _, record := range records {
				if record["type"] == "prompt" {
					prompts++
					if record["message"] != "captured [image1]" || len(record["images"].([]any)) != 1 {
						t.Fatalf("wrong captured submission: %v", record)
					}
					if busy && record["streamingBehavior"] != "steer" {
						t.Fatal("submission did not use authoritative busy state")
					}
				}
			}
			if prompts != 1 || tab.resume.pending || tab.resume.submit != nil {
				t.Fatalf("duplicate/lost submission: %v", records)
			}
		})
	}
}
