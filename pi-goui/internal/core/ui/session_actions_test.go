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
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/rpc"
	"pi-goui/internal/core/session"
)

func actionFile(t *testing.T, id string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "saved.jsonl")
	data := fmt.Sprintf("{\"type\":\"session\",\"version\":3,\"id\":%q,\"cwd\":\"/tmp\"}\n", id) + `{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"original message"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func actionKey(m *Model, code rune, mod tea.KeyMod) tea.Cmd {
	_, cmd := m.Update(tea.KeyPressMsg{Code: code, Mod: mod})
	return cmd
}
func selectedSaved(t *testing.T, m *Model, path, id string) *dialog {
	t.Helper()
	tab := m.current()
	m.sessionDialog(tab, []session.Summary{{ID: id, Path: path, Name: "Saved"}})
	d := tab.dialogs[0]
	d.editor.SetValue("Saved")
	return d
}
func TestSessionActionSavedRenameDeleteAndCancellation(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	p := actionFile(t, "saved-id")
	before, _ := os.ReadFile(p)
	selector := selectedSaved(t, m, p, "saved-id")
	actionKey(m, 'r', tea.ModCtrl)
	if len(tab.dialogs) != 2 || tab.dialogs[0].kind != "session-rename" || tab.dialogs[1] != selector {
		t.Fatal("rename not stacked in front of selector")
	}
	actionKey(m, tea.KeyEscape, 0)
	if len(tab.dialogs) != 1 || tab.dialogs[0] != selector {
		t.Fatal("cancel lost selector")
	}
	unchanged, _ := os.ReadFile(p)
	if string(before) != string(unchanged) {
		t.Fatal("cancel mutated history")
	}
	actionKey(m, 'r', tea.ModCtrl)
	tab.dialogs[0].editor.SetValue("Saved renamed")
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	data, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(data), string(before)) || !strings.Contains(string(data), `"name":"Saved renamed"`) {
		t.Fatal("rename did not persist native metadata")
	}
	if selector.editor.Value() != "Saved" || len(m.filtered(selector)) != 1 || !strings.Contains(m.filtered(selector)[0].Label, "renamed") {
		t.Fatal("rename lost query or selected row")
	}
	actionKey(m, 'd', tea.ModCtrl)
	if tab.dialogs[0].kind != "session-delete" {
		t.Fatal("delete did not confirm")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("deleted before Enter")
	}
	actionKey(m, tea.KeyEscape, 0)
	if _, err := os.Stat(p); err != nil {
		t.Fatal("Esc deleted history")
	}
	actionKey(m, 'd', tea.ModCtrl)
	result = commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("confirmed selected file remains")
	}
	if len(tab.dialogs) != 1 || len(m.filtered(selector)) != 0 || selector.editor.Value() != "Saved" {
		t.Fatal("delete did not retain/refresh selector")
	}
	// Duplicate completion cannot mutate the current session or remove another tab.
	tabs := len(m.tabs)
	m.Update(result)
	if len(m.tabs) != tabs {
		t.Fatal("duplicate completion changed tabs")
	}
}
func TestSessionActionKeyOverridesAndStableTarget(t *testing.T) {
	m := testModel(t, `{"app.session.rename":["alt+r"],"app.session.delete":[],"app.session.deleteNoninvasive":[]}`)
	p := actionFile(t, "saved-id")
	selector := selectedSaved(t, m, p, "saved-id")
	actionKey(m, 'r', tea.ModCtrl)
	if len(m.current().dialogs) != 1 {
		t.Fatal("default rename escaped override")
	}
	actionKey(m, 'd', tea.ModCtrl)
	if len(m.current().dialogs) != 1 {
		t.Fatal("disabled deletion still active")
	}
	selector.editor.SetValue("Saved")
	actionKey(m, 'r', tea.ModAlt)
	d := m.current().dialogs[0]
	if d.kind != "session-rename" {
		t.Fatal("remapped rename missing")
	}
	// The underlying filter changes while a subdialog is open: target stays bound.
	selector.editor.SetValue("no match")
	d.editor.SetValue("Bound target")
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "Bound target") {
		t.Fatal("rename lost original target")
	}
	if strings.Contains(m.sessionSelectorHelp(), "delete") || strings.Contains(m.sessionSelectorHelp(), "^r") {
		t.Fatal("help ignores overrides")
	}
}

// Entire fake process is isolated to this test executable. On EOF it appends a
// record while the history must still exist, proving unlink waits for shutdown.
func TestSessionActionRPCProcess(t *testing.T) {
	if os.Getenv("GOUI_ACTION_HELPER") == "" {
		return
	}
	p, id, wire, marker := os.Getenv("GOUI_ACTION_FILE"), os.Getenv("GOUI_ACTION_ID"), os.Getenv("GOUI_ACTION_WIRE"), os.Getenv("GOUI_ACTION_MARKER")
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		log, _ := os.OpenFile(wire, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if log != nil {
			_, _ = log.Write(append(append([]byte{}, scanner.Bytes()...), '\n'))
			_ = log.Close()
		}
		kind, _ := request["type"].(string)
		data := any(map[string]any{})
		var err error
		switch kind {
		case "get_state":
			data = map[string]any{"sessionId": id, "sessionFile": p}
		case "set_session_name":
			var f *session.MutationFile
			f, err = session.InspectMutationFile(p, id)
			if err == nil {
				err = f.Rename(request["name"].(string))
			}
		}
		response := map[string]any{"type": "response", "id": request["id"], "command": kind, "success": err == nil, "data": data}
		if err != nil {
			response["error"] = err.Error()
		}
		_ = encoder.Encode(response)
	}
	if marker != "" {
		value := "writer closed before unlink"
		if os.Getenv("GOUI_ACTION_REPLACE") != "" {
			_ = os.Rename(p, p+".preserved")
			_ = os.WriteFile(p, []byte("replacement must survive"), 0600)
		} else {
			f, err := session.InspectMutationFile(p, id)
			if err == nil {
				err = f.Rename("final writer flush")
			}
			if err != nil {
				value = err.Error()
			}
		}
		_ = os.WriteFile(marker, []byte(value), 0600)
	}
	os.Exit(0)
}
func actionClient(t *testing.T, p, id string, replace bool) (*rpc.Client, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake RPC wrapper")
	}
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wire, marker := filepath.Join(root, "wire"), filepath.Join(root, "closed")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	replacement := ""
	if replace {
		replacement = "1"
	}
	script := "#!/bin/sh\nGOUI_ACTION_HELPER=1 GOUI_ACTION_FILE=" + quote(p) + " GOUI_ACTION_ID=" + quote(id) + " GOUI_ACTION_WIRE=" + quote(wire) + " GOUI_ACTION_MARKER=" + quote(marker) + " GOUI_ACTION_REPLACE=" + quote(replacement) + " exec " + quote(exe) + " -test.run=^TestSessionActionRPCProcess$ -- \"$@\"\n"
	bin := filepath.Join(root, "fake-pi")
	if err = os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := rpc.Start(rpc.Config{Bin: bin, Cwd: root, AgentDir: root, SessionFile: p})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, wire, marker
}
func attachActionClient(t *testing.T, tab *tab, id string, replace bool) (string, string, string) {
	t.Helper()
	p := actionFile(t, id)
	c, wire, marker := actionClient(t, p, id, replace)
	tab.client = c
	tab.state.ID = id
	tab.state.File = p
	tab.state.Name = id
	tab.dead = false
	return p, wire, marker
}
func TestSessionActionRenamesSelectedOpenTabNotOrigin(t *testing.T) {
	m := testModel(t, "")
	origin := m.current()
	origin.setValue("origin draft")
	target := m.addTab("")
	p, wire, _ := attachActionClient(t, target, "other", false)
	target.setValue("other draft")
	m.activate(0)
	m.sessionDialog(origin, nil)
	origin.dialogs[0].editor.SetValue("other")
	actionKey(m, 'r', tea.ModCtrl)
	origin.dialogs[0].editor.SetValue("Actual selected name")
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	if target.state.Name != "Actual selected name" || origin.state.Name != "" || target.editor.Value() != "other draft" || origin.editor.Value() != "origin draft" {
		t.Fatal("rename modified wrong state/draft")
	}
	records := wireRecords(t, wire, 2)
	if records[1]["type"] != "set_session_name" {
		t.Fatal(records)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "Actual selected name") {
		t.Fatal("RPC rename did not persist")
	}
}
func TestSessionDeleteWaitsForWriterAndPreservesOtherTab(t *testing.T) {
	m := testModel(t, "")
	origin := m.current()
	origin.setValue("keep")
	origin.client, _ = fakeClient(t)
	target := m.addTab("")
	p, _, marker := attachActionClient(t, target, "other", false)
	target.state.Busy = true
	target.sending = true
	target.setValue("discard only after Enter")
	target.images = []map[string]any{{"data": "fake"}}
	m.activate(0)
	m.sessionDialog(origin, nil)
	origin.dialogs[0].editor.SetValue("other")
	actionKey(m, 'd', tea.ModCtrl)
	if !strings.Contains(origin.dialogs[0].message, "running work") {
		t.Fatal("no work-loss warning")
	}
	actionKey(m, tea.KeyEscape, 0)
	if target.mutation != nil || target.editor.Value() == "" {
		t.Fatal("cancel touched target")
	}
	actionKey(m, 'd', tea.ModCtrl)
	cmd := actionKey(m, tea.KeyEnter, 0)
	if !m.reservedSession(p) || m.closeSession(target) != nil || m.resume(p) != nil {
		t.Fatal("pending mutation was not reserved")
	}
	result := commandResult(cmd).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	markerData, _ := os.ReadFile(marker)
	if string(markerData) != "writer closed before unlink" {
		t.Fatalf("writer/unlink ordering: %s", markerData)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("history remains after confirmed deletion")
	}
	m.Update(result)
	if len(m.tabs) != 1 || m.current() != origin || origin.editor.Value() != "keep" {
		t.Fatal("delete harmed origin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := origin.client.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatal("other writer stopped", err)
	}
	m.Update(eventMsg{id: target.id, event: rpc.Event{Data: []byte(`{"type":"agent_start"}`)}})
	if len(m.tabs) != 1 || origin.state.Busy {
		t.Fatal("late deleted event leaked")
	}
}
func TestSessionDeleteLastTabAndFailureRetainHistoryDraft(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			m := testModel(t, "")
			target := m.current()
			p, _, _ := attachActionClient(t, target, "only", replace)
			target.setValue("retained on failure")
			oldID := target.id
			m.sessionDialog(target, nil)
			actionKey(m, 'd', tea.ModCtrl)
			result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
			m.Update(result) // Do not execute returned blank-session startup in this test.
			if len(m.tabs) != 1 {
				t.Fatal("last deletion left no current session")
			}
			if replace {
				if result.err == nil || m.current() != target || target.editor.Value() != "retained on failure" || !target.dead || target.id == oldID {
					t.Fatal("failed deletion lost draft or retained stale ID")
				}
				data, _ := os.ReadFile(p)
				if string(data) != "replacement must survive" {
					t.Fatal("deleted replaced file")
				}
				if _, err := os.Stat(p + ".preserved"); err != nil {
					t.Fatal("original history disappeared")
				}
				m.Update(snapshotMsg{id: oldID})
				if target.editor.Value() != "retained on failure" {
					t.Fatal("stale snapshot changed failed session")
				}
			} else {
				if result.err != nil {
					t.Fatal(result.err)
				}
				if m.current() == target || m.current().state.File != "" || m.current().editor.Value() != "" {
					t.Fatal("last deletion did not create a fresh blank slot")
				}
			}
		})
	}
}
func TestSessionActionHardlinkReservationAndOpenAlias(t *testing.T) {
	m := testModel(t, "")
	p := actionFile(t, "saved-id")
	alias := filepath.Join(filepath.Dir(p), "alias.jsonl")
	if err := os.Link(p, alias); err != nil {
		t.Fatal(err)
	}
	selector := selectedSaved(t, m, p, "saved-id")
	actionKey(m, 'r', tea.ModCtrl)
	m.current().dialogs[0].editor.SetValue("name")
	cmd := actionKey(m, tea.KeyEnter, 0)
	if !m.reservedSession(alias) || m.resume(alias) != nil || m.open(alias) != nil {
		t.Fatal("hardlink bypassed pending mutation")
	}
	result := commandResult(cmd).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	// A stale completion must not release a later reservation for the same path.
	later := &sessionAction{key: result.action.key, path: p}
	m.sessionActions[later.key] = later
	m.Update(result)
	if m.sessionActions[later.key] != later {
		t.Fatal("late result cleared a new operation")
	}
	delete(m.sessionActions, later.key)
	other := m.addTab(alias)
	other.state.ID = "saved-id"
	other.dead = true
	m.activate(0)
	selector.editor.SetValue("name")
	actionKey(m, 'd', tea.ModCtrl)
	if len(m.current().dialogs) != 1 || !strings.Contains(selector.message, "opened") {
		t.Fatal("saved hardlink bypassed existing writer detection")
	}
}

func TestSessionActionRefusesStartupAndChangedSelection(t *testing.T) {
	m := testModel(t, "")
	target := m.current()
	m.sessionDialog(target, nil)
	actionKey(m, 'd', tea.ModCtrl)
	if len(target.dialogs) != 1 || !strings.Contains(target.dialogs[0].message, "starting") {
		t.Fatal("allowed delete during pending startup")
	}
	target.dialogs = nil
	p := actionFile(t, "before")
	selector := selectedSaved(t, m, p, "before")
	actionKey(m, 'd', tea.ModCtrl)
	changed := actionFile(t, "after")
	if err := os.Rename(changed, p); err != nil {
		t.Fatal(err)
	}
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err == nil {
		t.Fatal("deleted different native session")
	}
	m.Update(result)
	if _, err := os.Stat(p); err != nil {
		t.Fatal("replacement deleted")
	}
	if !strings.Contains(selector.message, "failed") {
		t.Fatal("failure not visible")
	}
}
