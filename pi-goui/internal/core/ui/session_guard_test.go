package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

const guardUser = `{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"hi"}}` + "\n"
const guardStop = `{"type":"message","id":"a","parentId":"u","message":{"role":"assistant","content":[],"stopReason":"stop"}}` + "\n"

func guardFile(t *testing.T, tab *tab, data string) {
	t.Helper()
	if tab.state.File == "" {
		tab.state.File = filepath.Join(t.TempDir(), "session.jsonl")
	}
	if err := os.WriteFile(tab.state.File, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestSessionGuardNewUnpersistedSession(t *testing.T) {
	for _, fresh := range []bool{true, false} {
		m := testModel(t, "")
		tab := m.current()
		tab.newSession = fresh
		tab.state.File = filepath.Join(t.TempDir(), "not-yet-written.jsonl")
		m.sessionGuardResult(guardInspect(t, m, tab, false, false))
		if fresh {
			if len(tab.dialogs) != 0 || tab.sessionGuard.warning != "" {
				t.Fatal("new unpersisted session warned")
			}
			if blocked, _ := m.gateSession(tab); blocked {
				t.Fatal("new session input blocked")
			}
			// Once data exists, the new-session flag must not bypass inspection.
			guardFile(t, tab, guardUser)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			if len(tab.dialogs) != 1 {
				t.Fatal("persisted unfinished session bypassed guard")
			}
		} else if len(tab.dialogs) != 1 || tab.sessionGuard.warning != uncertainWarning {
			t.Fatal("missing existing session should still warn")
		}
	}
}

func guardInspect(t *testing.T, m *Model, tab *tab, submit, follow bool) sessionGuardMsg {
	t.Helper()
	cmd := m.inspectSession(tab, submit, follow, time.Now().Add(3*time.Second))
	if cmd == nil {
		t.Fatal("no inspection")
	}
	return cmd().(sessionGuardMsg)
}
func guardContinue(t *testing.T, m *Model, tab *tab) {
	t.Helper()
	if len(tab.dialogs) != 1 || tab.dialogs[0].selected != 0 || len(tab.dialogs[0].options) != 1 || tab.dialogs[0].options[0].Value != "continue" {
		t.Fatal("missing single confirmation option")
	}
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "enter")
	if len(tab.dialogs) != 0 || !tab.sessionGuard.approved {
		t.Fatal("Enter did not confirm and unlock session")
	}
}

func TestSessionGuardRejectsBrokenCompletedBranch(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardStop) // Its declared parent user message is missing.
	tab.setValue("keep this draft")
	v := guardInspect(t, m, tab, true, false)
	if v.err == nil {
		t.Fatal("broken parent chain accepted as completed")
	}
	m.sessionGuardResult(v)
	if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "session-send-confirm" || tab.sessionGuard.warning != uncertainWarning {
		t.Fatal("broken history did not require confirmation")
	}
	assertValue(t, tab, "keep this draft")
}

func TestSessionGuardBeforeInputAndCancel(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardUser)
	// Initial editor input is held until asynchronous inspection completes.
	cmd := m.editDraft(tab, "", tea.PasteMsg{Content: "blocked"})
	if cmd == nil || tab.editor.Value() != "" {
		t.Fatal("input accepted before inspection")
	}
	m.sessionGuardResult(cmd().(sessionGuardMsg))
	if len(tab.dialogs) != 1 {
		t.Fatal("empty draft did not get modal")
	}
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if len(tab.dialogs) != 0 {
		t.Fatal("tick reopened cancelled modal")
	}
	m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if tab.editor.Value() != "" || len(tab.dialogs) != 1 {
		t.Fatal("cancel failed to gate/reopen")
	}
	// Modal confirmation keys do not leak into the editor.
	m.Update(tea.PasteMsg{Content: "blocked modal paste"})
	if tab.editor.Value() != "" {
		t.Fatal("paste leaked through modal")
	}
	guardContinue(t, m, tab)
	m.editDraft(tab, "", tea.PasteMsg{Content: "allowed"})
	if tab.editor.Value() != "allowed" {
		t.Fatal("confirmation did not unlock")
	}
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if len(tab.dialogs) != 0 {
		t.Fatal("unchanged approved warning reprompted")
	}
}

func TestSessionGuardPreservesDraftAndSend(t *testing.T) {
	const draft = "draft [image1]"
	m := testModel(t, "")
	tab := m.current()
	c, wire := fakeClient(t)
	tab.client = c
	guardFile(t, tab, guardUser)
	tab.setValue(draft)
	tab.images = []map[string]any{{"type": "image", "data": "YQ==", "mimeType": "image/png"}}
	m.sessionGuardResult(m.submit(tab, true)().(sessionGuardMsg))
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
	if tab.editor.Value() != draft || len(tab.images) != 1 {
		t.Fatal("cancel discarded draft")
	}
	m.submit(tab, true)
	guardContinue(t, m, tab)
	if tab.editor.Value() != draft || len(tab.images) != 1 {
		t.Fatal("confirmation consumed draft or attachment")
	}
	// Fresh submit check retains intent; own run can start while it is pending.
	v := m.submit(tab, true)().(sessionGuardMsg)
	tab.state.Busy = true
	cmd := m.sessionGuardResult(v)
	if cmd == nil {
		t.Fatal("no send")
	}
	if reply := cmd().(replyMsg); reply.err != nil {
		t.Fatal(reply.err)
	}
	// A busy submit first retrieves the authoritative pending queue, then sends
	// the preserved follow-up intent (the fake RPC returns an empty queue).
	records := wireRecords(t, wire, 2)
	if len(records) != 2 || records[0]["type"] != "clear_queue" || records[1]["type"] != "prompt" || records[1]["streamingBehavior"] != "followUp" || records[1]["message"] != draft {
		t.Fatalf("wire: %#v", records)
	}
	images, ok := records[1]["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("attachment missing from prompt: %#v", records[1])
	}
	image, ok := images[0].(map[string]any)
	if !ok || image["type"] != "image" || image["data"] != "YQ==" || image["mimeType"] != "image/png" {
		t.Fatalf("attachment changed: %#v", images[0])
	}
}

func TestSessionGuardResetAndAsyncMutations(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardUser)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	guardContinue(t, m, tab)
	guardFile(t, tab, guardUser+guardStop)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if tab.sessionGuard.approved {
		t.Fatal("safe state retained approval")
	}
	guardFile(t, tab, guardUser)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if len(tab.dialogs) != 1 {
		t.Fatal("new unfinished state not prompted")
	}
	tab.dialogs = nil
	m.Update(externalMsg{id: tab.id, text: "external"})
	if tab.editor.Value() != "" {
		t.Fatal("external editor bypassed gate")
	}
	tab.dialogs = nil
	m.Update(clipboardImageMsg{id: tab.id, image: map[string]any{"type": "image"}})
	if len(tab.images) != 0 {
		t.Fatal("async image bypassed gate")
	}
	tab.dialogs = nil
	tab.state.File = filepath.Join(t.TempDir(), "other.jsonl")
	guardFile(t, tab, guardUser+guardStop)
	blocked, cmd := m.gateSession(tab)
	if !blocked || cmd == nil {
		t.Fatal("session switch did not reset gate")
	}
	m.sessionGuardResult(cmd().(sessionGuardMsg))
	if blocked, _ = m.gateSession(tab); blocked {
		t.Fatal("completed session still gated")
	}
	tab.state.Busy = true
	if blocked, _ = m.gateSession(tab); blocked {
		t.Fatal("own run gated")
	}
}

func TestSessionGuardFreshSubmitAndPartialFile(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	c, wire := fakeClient(t)
	tab.client = c
	guardFile(t, tab, guardUser+guardStop)
	tab.setValue("draft")
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	guardFile(t, tab, guardUser+guardStop+`{"type":`)
	m.sessionGuardResult(m.submit(tab, false)().(sessionGuardMsg))
	if len(tab.dialogs) != 1 || tab.sessionGuard.warning != uncertainWarning {
		t.Fatal("partial file did not gate")
	}
	guardContinue(t, m, tab)
	guardFile(t, tab, guardUser+guardStop)
	cmd := m.sessionGuardResult(m.submit(tab, false)().(sessionGuardMsg))
	if cmd == nil {
		t.Fatal("safe submit blocked")
	}
	if reply := cmd().(replyMsg); reply.err != nil {
		t.Fatal(reply.err)
	}
	if len(wireRecords(t, wire, 1)) != 1 {
		t.Fatal("wrong send count")
	}
}
