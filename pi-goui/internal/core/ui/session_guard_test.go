package ui

import (
	"os"
	"path/filepath"
	"strings"
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
			// Missing end markers are not evidence of a concurrent writer.
			guardFile(t, tab, guardUser)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			if len(tab.dialogs) != 0 || tab.sessionGuard.warning != "" {
				t.Fatal("valid unfinished session should not require confirmation")
			}
			guardFile(t, tab, guardUser+`{"type":`)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			if len(tab.dialogs) != 1 {
				t.Fatal("new-session flag bypassed malformed-history protection")
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

func TestUnfinishedSessionEditsAndSendsWithoutConfirmation(t *testing.T) {
	for name, history := range map[string]string{
		"user":               guardUser,
		"assistant-tool-use": guardUser + "{\"type\":\"message\",\"id\":\"a\",\"parentId\":\"u\",\"message\":{\"role\":\"assistant\",\"stopReason\":\"toolUse\"}}\n",
		"tool-result":        guardUser + "{\"type\":\"message\",\"id\":\"r\",\"parentId\":\"u\",\"message\":{\"role\":\"toolResult\"}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			c, wire := fakeClient(t)
			tab.client = c
			guardFile(t, tab, history)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			if len(tab.dialogs) != 0 || tab.sessionGuard.warning != "" {
				t.Fatal("unfinished history still triggers confirmation")
			}
			m.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if tab.editor.Value() != "x" {
				t.Fatalf("editing blocked: %q", tab.editor.Value())
			}
			cmd := m.sessionGuardResult(m.submit(tab, false)().(sessionGuardMsg))
			if cmd == nil {
				t.Fatal("send blocked")
			}
			if reply := cmd().(replyMsg); reply.err != nil {
				t.Fatal(reply.err)
			}
			records := wireRecords(t, wire, 1)
			if len(records) != 1 || records[0]["type"] != "prompt" || records[0]["message"] != "x" || len(tab.dialogs) != 0 {
				t.Fatalf("send changed or blocked: %#v, %d dialogs", records, len(tab.dialogs))
			}
		})
	}
}

func TestSessionGuardBeforeInputAndCancel(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardUser+`{"type":`)
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
	guardFile(t, tab, guardUser+`{"type":`)
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
	guardFile(t, tab, guardUser+`{"type":`)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	guardContinue(t, m, tab)
	guardFile(t, tab, guardUser+guardStop)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if tab.sessionGuard.approved {
		t.Fatal("safe state retained approval")
	}
	guardFile(t, tab, guardUser+`{"type":`)
	m.sessionGuardResult(guardInspect(t, m, tab, false, false))
	if len(tab.dialogs) != 1 {
		t.Fatal("new malformed-history state not prompted")
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

func TestSessionGuardCachesStableHistory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardUser)
	first := guardInspect(t, m, tab, false, false)
	if first.err != nil || first.history == nil {
		t.Fatalf("initial inspection: %v", first.err)
	}
	if tab.sessionGuard.history != nil {
		t.Fatal("background inspection changed live cache")
	}
	m.sessionGuardResult(first)
	cached := tab.sessionGuard.history
	for range 3 {
		next := guardInspect(t, m, tab, false, false)
		if next.err != nil || next.history != cached {
			t.Fatalf("unchanged history was rescanned: %v", next.err)
		}
		m.sessionGuardResult(next)
	}
}

func TestSessionGuardCacheInvalidation(t *testing.T) {
	for name, change := range map[string]func(*testing.T, string, os.FileInfo){
		"append": func(t *testing.T, path string, _ os.FileInfo) {
			if err := os.WriteFile(path, []byte(guardUser+guardStop+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"truncate": func(t *testing.T, path string, _ os.FileInfo) {
			if err := os.WriteFile(path, []byte(guardUser), 0600); err != nil {
				t.Fatal(err)
			}
		},
		"mtime": func(t *testing.T, path string, before os.FileInfo) {
			later := before.ModTime().Add(time.Second)
			if err := os.Chtimes(path, later, later); err != nil {
				t.Fatal(err)
			}
		},
		"permissions": func(t *testing.T, path string, _ os.FileInfo) {
			if err := os.Chmod(path, 0640); err != nil {
				t.Fatal(err)
			}
		},
		"replace-same-size-and-mtime": func(t *testing.T, path string, before os.FileInfo) {
			replacement := filepath.Join(t.TempDir(), "replacement.jsonl")
			if err := os.WriteFile(replacement, []byte(guardUser+guardStop), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(replacement, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, path); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			guardFile(t, tab, guardUser+guardStop)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			before := tab.sessionGuard.history
			if before == nil {
				t.Fatal("no successful cache")
			}
			change(t, tab.state.File, before)
			next := guardInspect(t, m, tab, false, false)
			if next.err != nil || next.history == nil || next.history == before || sameSessionHistory(before, next.history) {
				t.Fatalf("changed history reused inspection: %v", next.err)
			}
			m.sessionGuardResult(next)
			if len(tab.dialogs) != 0 {
				t.Fatal("valid changed history warned")
			}
		})
	}
}

func TestSessionGuardCacheFailureAndRecovery(t *testing.T) {
	for _, missing := range []bool{false, true} {
		m := testModel(t, "")
		tab := m.current()
		tab.newSession = false
		guardFile(t, tab, guardUser+guardStop)
		m.sessionGuardResult(guardInspect(t, m, tab, false, false))
		cached := tab.sessionGuard.history
		if missing {
			if err := os.Remove(tab.state.File); err != nil {
				t.Fatal(err)
			}
		} else {
			guardFile(t, tab, guardUser+`{"type":`)
		}
		failed := guardInspect(t, m, tab, false, false)
		if failed.err == nil || failed.history != nil {
			t.Fatal("failed inspection cached success")
		}
		m.sessionGuardResult(failed)
		if tab.sessionGuard.history != nil || tab.sessionGuard.warning != uncertainWarning {
			t.Fatal("failure retained successful cache")
		}
		guardFile(t, tab, guardUser+guardStop)
		// Even restoring old metadata cannot revive a cache invalidated by failure.
		if err := os.Chtimes(tab.state.File, cached.ModTime(), cached.ModTime()); err != nil {
			t.Fatal(err)
		}
		recovered := guardInspect(t, m, tab, false, false)
		if recovered.err != nil || recovered.history == nil || recovered.history == cached {
			t.Fatalf("recovery did not rescan: %v", recovered.err)
		}
		m.sessionGuardResult(recovered)
		if tab.sessionGuard.warning != "" {
			t.Fatal("repaired history retained warning")
		}
	}
}

func TestSessionGuardCacheRequiresStableRead(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	guardFile(t, tab, guardUser)
	before, err := os.Lstat(tab.state.File)
	if err != nil {
		t.Fatal(err)
	}
	guardFile(t, tab, guardUser+guardStop)
	if info, err := stableSessionHistory(tab.state.File, before); err == nil || info != nil {
		t.Fatal("file changed during inspection was approved")
	}
	before, err = os.Lstat(tab.state.File)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tab.state.File); err != nil {
		t.Fatal(err)
	}
	if info, err := stableSessionHistory(tab.state.File, before); err == nil || info != nil {
		t.Fatal("file removed during inspection was approved")
	}
}

func TestSessionGuardCacheRejectsNonRegularHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(guardUser), 0600); err != nil {
		t.Fatal(err)
	}
	cached, err := inspectSessionHistory(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked.jsonl")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if info, err := inspectSessionHistory(link, cached); err == nil || info != nil {
		t.Fatal("symlink reused successful cache")
	}
	if info, err := inspectSessionHistory(t.TempDir(), nil); err == nil || info != nil {
		t.Fatal("directory accepted as history")
	}
}

func TestSessionGuardCachedSubmitKeepsAsyncGuards(t *testing.T) {
	for name, mutate := range map[string]func(*tab){
		"draft":          func(tab *tab) { tab.setValue("changed draft") },
		"draft-revision": func(tab *tab) { tab.draftRevision++ },
		"images":         func(tab *tab) { tab.imageGeneration++ },
		"path":           func(tab *tab) { tab.state.File += ".other" },
		"sequence":       func(tab *tab) { tab.sessionGuard.sequence++ },
	} {
		t.Run(name, func(t *testing.T) {
			m := testModel(t, "")
			tab := m.current()
			c, _ := fakeClient(t)
			tab.client = c
			guardFile(t, tab, guardUser)
			m.sessionGuardResult(guardInspect(t, m, tab, false, false))
			cached := tab.sessionGuard.history
			tab.setValue("original draft")
			cmd := m.inspectSession(tab, true, true, time.Now())
			if cmd == nil {
				t.Fatal("no submit inspection")
			}
			mutate(tab)
			result := cmd().(sessionGuardMsg)
			if result.err != nil || result.history != cached {
				t.Fatalf("submit did not use stable cache: %v", result.err)
			}
			if m.sessionGuardResult(result) != nil {
				t.Fatal("cached submit bypassed async guard")
			}
		})
	}
}

func BenchmarkSessionGuardHistory(b *testing.B) {
	path := filepath.Join(b.TempDir(), "history.jsonl")
	raw := `{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"` + strings.Repeat("x", 1<<20) + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		b.Fatal(err)
	}
	cached, err := inspectSessionHistory(path, nil)
	if err != nil {
		b.Fatal(err)
	}
	for _, hit := range []bool{false, true} {
		name := "scan"
		var prior os.FileInfo
		if hit {
			name, prior = "unchanged", cached
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := inspectSessionHistory(path, prior); err != nil {
					b.Fatal(err)
				}
			}
		})
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
