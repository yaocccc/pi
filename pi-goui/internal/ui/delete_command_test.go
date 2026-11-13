package ui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDeleteCommandConfirmsAndKeepsStableTarget(t *testing.T) {
	m := testModel(t, "")
	target := m.current()
	path, _, marker := attachActionClient(t, target, "delete-current", false)
	target.state.Busy = true
	target.setValue("keep until confirmed")
	other := m.addTab("")
	otherPath, _, _ := attachActionClient(t, other, "keep-other", false)
	other.setValue("other draft")
	m.activate(0)

	if _, handled := m.slash(target, "/delete"); !handled {
		t.Fatal("delete command not handled locally")
	}
	if len(target.dialogs) != 1 || target.dialogs[0].kind != "session-delete" || target.dialogs[0].action.targetID != target.id {
		t.Fatal("delete must directly confirm the current session, without a selector")
	}
	if !strings.Contains(target.dialogs[0].message, "running work") {
		t.Fatal("missing destructive-operation warning")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("history deleted before confirmation")
	}
	actionKey(m, tea.KeyEscape, 0)
	if len(target.dialogs) != 0 || target.mutation != nil || target.editor.Value() != "keep until confirmed" {
		t.Fatal("cancel changed current session")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cancel stopped the RPC writer")
	}

	m.slash(target, "/delete")
	cmd := actionKey(m, tea.KeyEnter, 0)
	if cmd == nil || !m.reservedSession(path) {
		t.Fatal("confirmed deletion did not reserve its target")
	}
	m.activate(1)
	result := commandResult(cmd).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("confirmed current-session history remains")
	}
	closed, err := os.ReadFile(marker)
	if err != nil || string(closed) != "writer closed before unlink" {
		t.Fatalf("did not wait for writer shutdown: %q, %v", closed, err)
	}
	if m.current() != other || len(m.tabs) != 1 || other.editor.Value() != "other draft" {
		t.Fatal("deletion followed the active tab instead of the captured target")
	}
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatal("deleted another session")
	}
}

func TestDeleteCommandLastSession(t *testing.T) {
	m := testModel(t, "")
	target := m.current()
	path, _, _ := attachActionClient(t, target, "only", false)
	target.setValue("/delete")
	m.submitChecked(target, false, true)
	if len(target.dialogs) != 1 || target.dialogs[0].kind != "session-delete" || target.editor.Value() != "" {
		t.Fatal("submitting /delete did not open confirmation")
	}
	result := commandResult(actionKey(m, tea.KeyEnter, 0)).(sessionActionMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result) // Do not start the replacement's real RPC in this test.
	if len(m.tabs) != 1 || m.current() == target || m.current().state.File != "" {
		t.Fatal("deleting the final session did not leave a new blank tab")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("deleted history remains")
	}
}

func TestDeleteCommandRejectsArgumentsAndUnsafeTargets(t *testing.T) {
	m := testModel(t, "")
	target := m.current()
	if cmd, handled := m.slash(target, "/delete another-session"); cmd != nil || !handled || len(target.dialogs) != 0 || !strings.Contains(target.toast.text, "Usage") {
		t.Fatal("delete accepted a target argument")
	}
	m.slash(target, "/delete")
	if len(target.dialogs) != 0 || !strings.Contains(target.toast.text, "starting") {
		t.Fatal("startup rejection was hidden in an invisible selector")
	}
	path, _, _ := attachActionClient(t, target, "same-file", false)
	other := m.addTab(path)
	other.state.ID, other.state.File = target.state.ID, path
	m.activate(0)
	m.slash(target, "/delete")
	if len(target.dialogs) != 0 || !strings.Contains(target.toast.text, "aliases") {
		t.Fatal("open alias did not block deletion with a visible warning")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("rejected deletion removed history")
	}
	for _, command := range localCommands {
		if command.Name == "delete" {
			return
		}
	}
	t.Fatal("delete is missing from command completion")
}
