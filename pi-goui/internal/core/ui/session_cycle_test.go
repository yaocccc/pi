package ui

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	rpc "pi-goui/internal/runtime"
)

func TestSessionPresenceDismissesPreviousRunOnForeignOwnership(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	now := time.Now()
	owner.setValue("unsent local draft")
	owner.working = workingState{Present: true, Turn: 2, Started: now.Add(-time.Second), Ended: now}
	owner.assistantStats = map[string]string{"local-answer": "saved answer statistics"}
	owner.cycle.probeSequence = 1
	presence := sessionPresenceMsg{owner: owner, file: owner.state.File, generation: owner.cycle.generation, sequence: 1}
	m.sessionPresence(presence)
	if !owner.working.Present || !strings.Contains(m.workingLine(owner), "2 turns") {
		t.Fatal("ordinary idle polling dismissed the local completion")
	}

	presence.status = rpc.SessionStatus{Busy: true, OwnerPID: 123}
	presence.sequence = 0
	m.sessionPresence(presence)
	if !owner.working.Present {
		t.Fatal("a stale ownership reply dismissed the local completion")
	}
	presence.sequence = 1
	m.sessionPresence(presence)
	if owner.working.Present || !strings.Contains(m.status(owner), "held by another instance") {
		t.Fatal("foreign ownership did not replace and dismiss the previous local completion")
	}

	presence.status = rpc.SessionStatus{}
	m.sessionPresence(presence)
	if got := m.workingLine(owner); got != "" {
		t.Fatalf("foreign writer finished but working row was not cleared: %q", got)
	}
	if owner.editor.Value() != "unsent local draft" || owner.assistantStats["local-answer"] != "saved answer statistics" || owner.client != nil {
		t.Fatal("handoff changed the draft, saved answer statistics or backend")
	}
}

func TestSessionPollingUsesOneSecondInterval(t *testing.T) {
	m, owner := previewModel(t)
	now := time.Now()
	owner.cycle.probed = now
	if cmd := m.pollSession(owner, now.Add(time.Second-time.Nanosecond), false); cmd != nil {
		t.Fatal("session polled before one second elapsed")
	}
	if cmd := m.pollSession(owner, now.Add(time.Second), false); cmd == nil {
		t.Fatal("session did not poll after one second")
	}
	if cmd := m.pollSession(owner, now.Add(2*time.Second), true); cmd != nil {
		t.Fatal("polling overlapped an outstanding probe")
	}
	owner.cycle.probePending = false
	if cmd := m.pollSession(owner, now, true); cmd == nil {
		t.Fatal("forced refresh waited for the polling interval")
	}
	if owner.client != nil || len(m.pool.clients) != 0 {
		t.Fatal("polling started a backend")
	}
}

func TestPersistentIdleAndFocusNeverStartPi(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, owner := previewModel(t)
	owner.setValue("local draft")
	cmd := m.start(owner)
	batch := cmd().(tea.BatchMsg)
	for _, child := range batch {
		m.Update(child())
	}
	if owner.client != nil || owner.cycle.phase != cycleIdle || len(m.pool.clients) != 0 || !owner.resume.preview {
		t.Fatal("opening saved history acquired a backend")
	}
	other := m.addTab("")
	m.Update(tickMsg(time.Now()))
	if owner.cycle.probePending || !other.cycle.probePending {
		t.Fatal("tick did not limit idle polling to the foreground tab")
	}
	m.activate(0)
	if owner.cycle.probePending {
		t.Fatal("focus armed a probe without returning its command")
	}
	m.Update(tickMsg(time.Now()))
	if !owner.cycle.probePending || owner.client != nil || len(m.pool.clients) != 0 || owner.editor.Value() != "local draft" {
		t.Fatal("focus did not schedule a read-only foreground poll")
	}
}

func TestOccupiedSessionSilentlyRejectsAllBackendActions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, owner := previewModel(t)
	owner.setValue("keep [image1]")
	owner.images = []map[string]any{{"data": "local-image"}}
	lease, err := rpc.AcquireSession(m.cfg.AgentDir, owner.state.File)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	m.Update(m.pollSession(owner, time.Now(), true)())
	if !owner.readOnly() || owner.ownRunActive() || len(owner.dialogs) != 0 || !strings.Contains(m.status(owner), "Read-only") {
		t.Fatal("another owner was projected as local work or a modal")
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEscape}, {Code: 'l', Mod: tea.ModCtrl}, {Code: 'p', Mod: tea.ModCtrl}, {Code: 'x', Text: "x"}} {
		m.key(key)
	}
	for _, op := range []string{"prompt", "abort", "compact", "clear_queue", "set_model", "set_thinking_level", "set_session_name"} {
		if m.requestCall(owner, op, nil) != nil {
			t.Fatalf("readonly dispatched %s", op)
		}
	}
	m.editDraft(owner, "", tea.PasteMsg{Content: "replacement"})
	if owner.editor.Value() != "keep [image1]" || len(owner.images) != 1 || owner.images[0]["data"] != "local-image" || owner.client != nil || owner.resume.submit != nil || owner.interruptRequested {
		t.Fatal("readonly input changed a draft or armed a write")
	}
	if m.requestCall(owner, "get_entries", nil) == nil {
		t.Fatal("readonly history browsing was disabled")
	}
}

func TestAcquisitionLoserRestoresCapturedDraftWithoutRetry(t *testing.T) {
	m, owner := previewModel(t)
	owner.setValue("original [image1]")
	owner.images = []map[string]any{{"data": "original"}}
	cmd := m.submitChecked(owner, false, true)
	if cmd == nil || owner.resume.submit == nil || owner.editor.Value() != "" {
		t.Fatal("send did not capture exactly one intent")
	}
	owner.setValue("later [image1]")
	owner.images = []map[string]any{{"data": "later"}}
	m.Update(openedMsg{id: owner.id, generation: owner.cycle.generation, err: &rpc.SessionBusyError{Status: rpc.SessionStatus{Busy: true, OwnerPID: 42}}})
	if owner.editor.Value() != "original [image1]\n\nlater [image2]" || len(owner.images) != 2 || owner.resume.submit != nil || owner.cycle.intent != nil || owner.cycle.phase != cycleIdle || !owner.readOnly() || len(owner.dialogs) != 0 {
		t.Fatal("lost lock race did not preserve local intent and stop retrying")
	}
	if m.submitChecked(owner, false, true) != nil {
		t.Fatal("busy observer queued another operation")
	}
}

func TestReleaseCheckAndReplyStayWithCycle(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	old, _ := fakeClient(t)
	current, _ := fakeClient(t)
	owner.client = current
	owner.cycle = sessionCycle{phase: cycleActive, generation: 2, primed: true}
	owner.resume.authoritative = true
	owner.setValue("retained draft")
	stale := releaseCheckMsg{id: owner.id, client: old, generation: 1, revision: owner.revision, idle: true}
	if m.releaseChecked(stale) != nil {
		t.Fatal("old check closed new client")
	}
	owner.cycle.inflight = 1
	m.Update(replyMsg{id: owner.id, client: old, generation: 1, op: "prompt", text: "old text", err: errors.New("old outcome")})
	if owner.editor.Value() != "retained draft" || owner.cycle.inflight != 1 {
		t.Fatal("old reply changed new cycle")
	}
	owner.cycle.inflight = 0
	v := releaseCheckMsg{id: owner.id, client: current, generation: 2, revision: owner.revision, idle: true}
	owner.dialogs = []*dialog{{requestID: "question"}}
	if m.releaseChecked(v) != nil {
		t.Fatal("release ignored pending extension question")
	}
	owner.dialogs = nil
	owner.state.FollowUp = []string{"queued"}
	if m.releaseChecked(v) != nil {
		t.Fatal("release ignored queued input")
	}
	owner.state.FollowUp = nil
	owner.revision++
	if m.releaseChecked(v) != nil {
		t.Fatal("release ignored an intervening event")
	}
	v.revision = owner.revision
	v.idle, v.reason = false, "background worker"
	if m.releaseChecked(v) != nil || !strings.Contains(owner.status, "background worker") {
		t.Fatal("background activity lost its lease or explanation")
	}
	v.idle = true
	closeCmd := m.releaseChecked(v)
	if closeCmd == nil || owner.cycle.phase != cycleClosing || owner.client != current {
		t.Fatal("release unlocked before actual wait")
	}
	m.closeEventStream(owner, nil)
	if owner.dead {
		t.Fatal("normal close was shown as stopped")
	}
	closed := closeCmd().(cycleClosedMsg)
	if closed.err != nil {
		t.Fatal(closed.err)
	}
	m.cycleClosed(closed)
	if owner.client != nil || owner.dead || owner.cycle.phase != cycleIdle || owner.status != "Idle" || owner.editor.Value() != "retained draft" {
		t.Fatal("clean close did not restore the same usable idle tab")
	}
}

func TestPrimingFailureClosesStartedBackendAndRestoresIntent(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	client, wire := fakeClient(t)
	owner.client = client
	owner.cycle = sessionCycle{phase: cycleActive, generation: 1, priming: true}
	owner.resume.pending = true
	owner.resume.submit = &resumeSubmission{original: "never dispatched"}
	cmd := m.cyclePrimed(cyclePrimedMsg{id: owner.id, client: client, generation: 1, err: errors.New("unsupported activity capability")})
	if cmd == nil || owner.client != client || owner.cycle.phase != cycleClosing || owner.editor.Value() != "never dispatched" || owner.resume.submit != nil {
		t.Fatal("failed priming lost the backend before closing it")
	}
	closed := cmd().(cycleClosedMsg)
	if closed.err != nil {
		t.Fatal(closed.err)
	}
	m.cycleClosed(closed)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := client.Call(ctx, map[string]any{"type": "get_state"}); err == nil {
		t.Fatal("priming failure leaked Pi")
	}
	raw, _ := os.ReadFile(wire)
	if strings.Contains(string(raw), `"type":"prompt"`) {
		t.Fatal("failed priming dispatched a business operation")
	}
}

func TestShortFileMutationUsesSameExclusiveLeaseWithoutStartingPi(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t, "")
	file := actionFile(t, "short-write")
	before, _ := os.ReadFile(file)
	lease, err := rpc.AcquireSession(m.cfg.AgentDir, file)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	origin := m.current()
	a := &sessionAction{kind: "rename", key: sessionPath(file), path: file, sessionID: "short-write", originID: origin.id}
	d := &dialog{action: a}
	origin.dialogs = []*dialog{d}
	msg := m.beginSessionAction(origin, d, "new title")().(sessionActionMsg)
	if !errors.Is(msg.err, rpc.ErrSessionBusy) {
		t.Fatalf("short write bypassed writer lease: %v", msg.err)
	}
	after, _ := os.ReadFile(file)
	if string(after) != string(before) || len(m.pool.clients) != 0 {
		t.Fatal("short write changed history or started Pi")
	}
}

func TestInitializingOwnerDoesNotStartAnotherBackend(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	client, _ := fakeClient(t)
	owner.client = client
	owner.cycle = sessionCycle{phase: cycleActive, generation: 1, priming: true}
	owner.resume.pending = true
	owner.resume.submit = &resumeSubmission{original: "captured once"}
	for _, cmd := range []tea.Cmd{m.requestCall(owner, "get_available_models", nil), m.cycleBackward(owner), m.beginCycle(owner, &writeIntent{run: func() tea.Cmd { return nil }})} {
		if cmd != nil {
			t.Fatal("initialization admitted a second start")
		}
	}
	if m.cwdEditable(owner) {
		t.Fatal("cwd switch could discard the captured startup intent")
	}
	if owner.client != client || owner.cycle.generation != 1 || owner.resume.submit.original != "captured once" || owner.cycle.intent != nil {
		t.Fatal("pending operation changed backend ownership")
	}
}

func TestReadOnlyDraftCanCloseWithConfirmation(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	owner.cycle.presence = rpc.SessionStatus{Busy: true, OwnerPID: 42}
	owner.setValue("do not discard silently")
	m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if len(owner.dialogs) != 1 || owner.dialogs[0].kind != "close" || owner.editor.Value() != "do not discard silently" {
		t.Fatal("readonly draft trapped exit or was silently discarded")
	}
	m.dialogKey(owner, tea.KeyPressMsg{Code: tea.KeyEscape}, "esc")
	if len(owner.dialogs) != 0 || owner.editor.Value() != "do not discard silently" {
		t.Fatal("cancel lost readonly draft")
	}
	m.key(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	owner.dialogs[0].selected = 1
	if cmd := m.confirmDialog(owner, owner.dialogs[0]); cmd == nil {
		t.Fatal("confirmed readonly close could not exit")
	}
}

func TestDiskRefreshRetainsViewportSelectionAndDraft(t *testing.T) {
	m := testModel(t, "")
	m.height = 10
	owner := m.current()
	owner.setValue("draft")
	owner.follow = false
	owner.folds["b"] = true
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "unchanged text"
	}
	first := render.Document{Text: strings.Join(lines, "\n"), Blocks: []render.Block{{ID: "a", Start: 0, End: 10}, {ID: "b", Start: 10, End: 30}}}
	m.publishDocument(owner, first, lines)
	owner.offset = 12
	owner.selection = transcriptSelection{anchor: textPoint{line: 2}, end: textPoint{line: 3, cell: 3}, moved: true, document: first.Text, lines: lines}
	appended := append(append([]string{}, lines...), "new saved response")
	next := render.Document{Text: strings.Join(appended, "\n"), Blocks: []render.Block{{ID: "a", Start: 0, End: 10}, {ID: "b", Start: 10, End: 31}}}
	m.publishDocument(owner, next, appended)
	if owner.offset != 12 || !owner.selection.moved || owner.selection.text() == "" {
		t.Fatal("background append disturbed viewport or unchanged selection")
	}
	inserted := append([]string{"insert1", "insert2", "insert3", "insert4"}, appended...)
	next = render.Document{Text: strings.Join(inserted, "\n"), Blocks: []render.Block{{ID: "a", Start: 0, End: 14}, {ID: "b", Start: 14, End: 35}}}
	m.publishDocument(owner, next, inserted)
	if owner.offset != 16 || owner.follow || owner.editor.Value() != "draft" || !owner.folds["b"] {
		t.Fatal("background refresh lost message anchor or local UI state")
	}
}

func TestStaleDiskPreviewReleasesItsPendingSlot(t *testing.T) {
	m, owner := previewModel(t)
	result := m.previewSession(owner)().(resumePreviewMsg)
	owner.revision++ // A short rename can finish while a large preview is parsing.
	m.receiveResumePreview(result)
	if owner.cycle.previewPending || len(owner.state.Items) != 0 {
		t.Fatal("stale preview applied or permanently blocked polling")
	}
	if m.previewSession(owner) == nil {
		t.Fatal("stale preview did not permit the next poll")
	}
}

func TestHandledInputDoesNotAwaitAnAgentStart(t *testing.T) {
	m := testModel(t, "")
	owner := m.current()
	client, _ := fakeClient(t)
	owner.client = client
	owner.cycle = sessionCycle{phase: cycleActive, generation: 1, primed: true}
	owner.resume.authoritative = true
	owner.setValue("extension handles this")
	reply := m.sendPrompt(owner, owner.editor.Value(), nil, false, true)().(replyMsg)
	m.Update(reply)
	if owner.awaitingStart || owner.sending || owner.cycle.inflight != 0 {
		t.Fatal("handled native input kept the task cycle pending forever")
	}
}
