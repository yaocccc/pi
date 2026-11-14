package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
	rpc "pi-goui/internal/runtime"
)

// Permit the same isolated test executable to host the real shared transport.
// Ordinary UI tests remain direct; only the dedicated subprocess enables it.
func TestMain(m *testing.M) {
	if handled, err := rpc.RunSharedSessionHelper(os.Args[1:]); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSharedSessionFileMutationLeaseAcrossInstances(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix session sharing")
	}
	if os.Getenv("GOUI_SHARED_UI_MUTATION_TEST") != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSharedSessionFileMutationLeaseAcrossInstances$", "-test.count=1")
		root := t.TempDir()
		cmd.Env = append(os.Environ(), "GOUI_SHARED_UI_MUTATION_TEST=1", "HOME="+root, "PI_CODING_AGENT_DIR="+filepath.Join(root, "agent"), "PI_OFFLINE=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shared UI mutation subprocess: %v\n%s", err, out)
		}
		return
	}
	rpc.EnableSessionSharing()
	p := actionFile(t, "shared")
	cfg, _, _ := actionRPCConfig(t, p, "shared", false, false)
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
	second, err := rpc.Start(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if !first.Shared() || !second.Shared() || first.PID() != second.PID() {
		t.Fatal("fixture did not share one real backend")
	}
	m := testModel(t, "")
	m.cfg.AgentDir = cfg.AgentDir
	m.opts.Bin = cfg.Bin
	tab := m.current()
	tab.client = first
	tab.state.ID = "shared"
	tab.state.File = p
	tab.setValue("keep local draft")
	m.requestDeleteSession(tab)
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err == nil || result.closed {
		t.Fatal("shared deletion was not refused before closing its client")
	}
	m.Update(result)
	if tab.dead || tab.editor.Value() != "keep local draft" {
		t.Fatal("failed shared delete retired the local session/draft")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("shared history was removed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := first.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatal(err)
	}
	// A saved row not open in this UI still joins the external session and
	// refuses its lease; it must never bypass sharing with a direct unlink.
	offline := testModel(t, "")
	offline.cfg.AgentDir = cfg.AgentDir
	selectedSaved(t, offline, p, "shared")
	actionKey(offline, 'd', tea.ModCtrl)
	result = commandResult(actionKey(offline, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err == nil || result.closed {
		t.Fatal("offline saved row bypassed external shared writer")
	}
	offline.Update(result)
	if _, err := second.Call(ctx, map[string]any{"type": "get_state"}); err != nil {
		t.Fatal("offline mutation stopped external writer", err)
	}
	// Online rename remains an RPC mutation, not a destructive file lease.
	m.sessionDialog(tab, nil)
	tab.dialogs[0].editor.SetValue("shared")
	actionKey(m, 'r', tea.ModCtrl)
	tab.dialogs[0].editor.SetValue("shared renamed")
	result = commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	deadline := time.After(3 * time.Second)
waitForRename:
	for {
		select {
		case event := <-second.Events():
			var message struct{ Type string }
			_ = json.Unmarshal(event.Data, &message)
			if message.Type == "goui_session_snapshot" {
				continue // Initial attachment refresh hint, not a rename.
			}
			if message.Type != "goui_session_changed" {
				t.Fatalf("rename was not broadcast: %s", event.Data)
			}
			other := testModel(t, "")
			other.cfg.AgentDir = cfg.AgentDir
			view := other.current()
			view.client, view.state.ID, view.state.File = second, "shared", p
			view.setValue("second instance local draft")
			_, refresh := other.sharedSessionEvent(view, event.Data)
			snapshot := refresh().(snapshotMsg)
			if snapshot.err != nil || snapshot.prepareErr != nil {
				t.Fatalf("other-instance refresh: %v / %v", snapshot.err, snapshot.prepareErr)
			}
			other.Update(snapshot)
			if view.state.Name != "shared renamed" || view.editor.Value() != "second instance local draft" {
				t.Fatal("remote rename failed to synchronize or overwrote local draft")
			}
			break waitForRename
		case <-deadline:
			t.Fatal("rename did not notify other instance")
		}
	}
	_ = second.Close()
	m.requestDeleteSession(tab)
	result = commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil || !result.closed {
		t.Fatalf("sole-client lease/delete failed: %+v", result)
	}
	m.Update(result)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("sole shared history was not deleted under lease", err)
	}
}

func TestSharedSessionChangedRefreshPreservesLocalDraftAndTabs(t *testing.T) {
	m := testModel(t, "")
	origin := m.current()
	origin.setValue("active local draft")
	target := m.addTab("")
	target.client, _ = fakeClient(t)
	target.state.ID = "fake"
	target.setValue("background unsent draft")
	target.images = []map[string]any{{"type": "image", "data": "YQ==", "mimeType": "image/png"}}
	images := append([]map[string]any(nil), target.images...)
	m.activate(0)
	active, revision := m.active, target.revision
	handled, cmd := m.sharedSessionEvent(target, json.RawMessage(`{"type":"goui_session_changed"}`))
	if !handled || cmd == nil || target.revision != revision+1 || !target.refreshing {
		t.Fatal("shared change did not request an owned snapshot")
	}
	msg := cmd().(snapshotMsg)
	if msg.err != nil || msg.prepareErr != nil {
		t.Fatalf("refresh failed: %v / %v", msg.err, msg.prepareErr)
	}
	m.Update(msg)
	if m.active != active || m.current() != origin || target.editor.Value() != "background unsent draft" || origin.editor.Value() != "active local draft" || !reflect.DeepEqual(target.images, images) {
		t.Fatal("shared refresh modified local draft, attachments or active tab")
	}
	if target.state.Model.ID != "one" || target.state.Thinking != "medium" {
		t.Fatalf("authoritative metadata was not synchronized: %+v", target.state)
	}
	if len(target.state.Items) != 0 {
		t.Fatal("shared control event appeared as a chat message")
	}
}

func TestSharedSessionSnapshotHintPreservesLocalPresentation(t *testing.T) {
	m := testModel(t, "")
	origin := m.current()
	origin.setValue("foreground draft")
	target := m.addTab("")
	target.client, _ = fakeClient(t)
	target.state.ID = "fake"
	target.title = "local tab title"
	target.setValue("background draft")
	target.images = []map[string]any{{"data": "local attachment"}}
	m.panelResize.sidebar, m.panelResize.inspector = 31, 47
	m.activate(0)
	layout, revision, draftRevision := m.panelResize, target.revision, target.draftRevision
	handled, refresh := m.sharedSessionEvent(target, json.RawMessage(`{"type":"goui_session_snapshot"}`))
	if !handled || refresh == nil || target.revision != revision+1 {
		t.Fatal("snapshot hint did not invalidate and refresh backend projection")
	}
	result := refresh().(snapshotMsg)
	if result.err != nil || result.prepareErr != nil {
		t.Fatalf("snapshot hint refresh: %v / %v", result.err, result.prepareErr)
	}
	m.Update(result)
	if m.current() != origin || len(m.tabs) != 2 || target.title != "local tab title" || m.panelResize != layout {
		t.Fatal("snapshot changed local tabs, title or layout")
	}
	if origin.editor.Value() != "foreground draft" || target.editor.Value() != "background draft" || target.draftRevision != draftRevision || len(target.images) != 1 || target.images[0]["data"] != "local attachment" {
		t.Fatal("snapshot consumed or replaced local draft/attachments")
	}
}

func TestSharedSessionChangedInvalidatesInflightSnapshot(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.client, _ = fakeClient(t)
	tab.state.ID = "fake"
	tab.state.Name = "current name"
	tab.setValue("draft")
	first := m.refresh(tab)
	msg := first().(snapshotMsg)
	before := tab.revision
	_, cmd := m.sharedSessionEvent(tab, json.RawMessage(`{"type":"goui_session_changed"}`))
	if cmd != nil || !tab.refreshAgain || tab.revision != before+1 {
		t.Fatal("in-flight refresh was not invalidated/coalesced")
	}
	m.Update(msg)
	if tab.state.Name != "current name" || tab.editor.Value() != "draft" || !tab.refreshing {
		t.Fatal("stale snapshot replaced local/current state or lost follow-up refresh")
	}
}

func TestSharedSessionNativeEventsKeepDraftAndRunState(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.setValue("unsubmitted local draft")
	tab.images = []map[string]any{{"data": "local-image"}}
	for _, raw := range []string{
		`{"type":"agent_start"}`,
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"sent from another goui"}]}}`,
		`{"type":"message_end","message":{"role":"user","content":[{"type":"text","text":"sent from another goui"}]}}`,
	} {
		m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: json.RawMessage(raw)}})
	}
	if !tab.state.Busy {
		t.Fatal("remote run did not set busy")
	}
	found := false
	for _, item := range tab.state.Items {
		if item.Role == "user" && item.Text == "sent from another goui" {
			found = true
		}
	}
	if !found {
		t.Fatal("remote user message did not reach this tab")
	}
	tab.sending, tab.awaitingStart = true, true
	m.Update(eventMsg{id: tab.id, event: rpc.Event{Data: json.RawMessage(`{"type":"agent_settled"}`)}})
	if tab.state.Busy || tab.sending || tab.awaitingStart {
		t.Fatal("settled shared run left local send state stuck")
	}
	if tab.editor.Value() != "unsubmitted local draft" || len(tab.images) != 1 {
		t.Fatal("remote submission consumed local draft/attachments")
	}
}

func TestSharedSessionQuestionnaireReplayAndRemoteResolution(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	client, wire := fakeClient(t)
	tab.client = client
	tab.setValue("local draft")
	q := d.extension.Handler.(*hostui.Dialog).Questionnaire
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeySpace}, "")
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "")
	if q.Active != 1 || !q.Answers[0].Completed {
		t.Fatal("fixture did not retain a completed first answer")
	}
	// Open the second question's free-text editor and leave it unfinished.
	d.selected = 2
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "")
	d.editor.SetValue("partial local answer")
	before, _ := json.Marshal(q)
	deadline := d.deadline
	m.extension(tab, json.RawMessage(`{"type":"extension_ui_request","id":"question-1","method":"input","title":"Replay","defaultValue":"must not replace","timeout":1}`))
	after, _ := json.Marshal(q)
	if len(tab.dialogs) != 1 || tab.dialogs[0] != d || d.extension.Handler.(*hostui.Dialog).Questionnaire != q || string(before) != string(after) || d.editor.Value() != "partial local answer" || d.deadline != deadline {
		t.Fatal("pending replay reset questionnaire answers, editor or timeout")
	}
	local := m.newDialog("model", "Local selector", "")
	tab.dialogs = append(tab.dialogs, local)
	handled, cmd := m.sharedSessionEvent(tab, json.RawMessage(`{"type":"goui_session_ui_resolved","id":"question-1"}`))
	if !handled || len(tab.dialogs) != 1 || tab.dialogs[0] != local || tab.editor.Value() != "local draft" {
		t.Fatal("remote answer did not close only its questionnaire")
	}
	// Execute even the focus command: closing remotely must never invoke the
	// extension's Cancel/Close callback and echo a cancelled response.
	commandResult(cmd)
	if data, err := os.ReadFile(wire); err == nil && len(data) > 0 {
		t.Fatalf("remote questionnaire resolution sent a response: %s", data)
	}
}

func TestSharedSessionUIResolutionOnlyRemovesMatchingQuestion(t *testing.T) {
	m := testModel(t, "")
	origin := m.current()
	origin.setValue("active draft")
	target := m.addTab("")
	target.setValue("other draft")
	client, wire := fakeClient(t)
	target.client = client
	m.activate(0)
	m.extension(target, json.RawMessage(`{"type":"extension_ui_request","id":"q1","method":"input","title":"Shared question"}`))
	question := target.dialogs[0]
	question.editor.SetValue("partial local answer")
	m.extension(target, json.RawMessage(`{"type":"extension_ui_request","id":"q1","method":"input","title":"Replay"}`))
	if len(target.dialogs) != 1 || target.dialogs[0] != question || question.editor.Value() != "partial local answer" {
		t.Fatal("pending replay replaced a local answer or duplicated question")
	}
	local := m.newDialog("model", "Local selector", "")
	target.dialogs = append(target.dialogs, local)
	m.extension(target, json.RawMessage(`{"type":"extension_ui_request","id":"q2","method":"confirm","title":"Other question"}`))
	_, cmd := m.sharedSessionEvent(target, json.RawMessage(`{"type":"goui_session_ui_resolved","id":"q1"}`))
	if cmd != nil || m.current() != origin || len(target.dialogs) != 2 || target.dialogs[0] != local || target.dialogs[1].requestID != "q2" {
		t.Fatal("resolution stole focus or removed unrelated dialog")
	}
	m.sharedSessionEvent(target, json.RawMessage(`{"type":"goui_session_ui_resolved","id":"q1"}`))
	m.sharedSessionEvent(target, json.RawMessage(`{"type":"goui_session_ui_resolved","id":""}`))
	if len(target.dialogs) != 2 || target.editor.Value() != "other draft" || origin.editor.Value() != "active draft" {
		t.Fatal("duplicate/empty resolution changed local state")
	}
	if data, err := os.ReadFile(wire); err == nil && len(data) > 0 {
		t.Fatalf("remote resolution sent a duplicate/cancel answer: %s", data)
	}
}
