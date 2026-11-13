package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func branchFixture(t *testing.T) (*Model, *tab, *tab, json.RawMessage) {
	t.Helper()
	m, source, _, snapshot := uiForkFixture(t)
	fork, err := session.CreateFork(source.state.File, source.state.ID, source.state.Cwd, "Branch", "old", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	m.forkCreated(forkCreatedMsg{id: source.id, fork: fork}) // Do not launch a real process.
	branch := m.current()
	m.document(branch)
	return m, source, branch, snapshot
}

func TestOriginReusesSourceWithoutChangingDraftOrRPC(t *testing.T) {
	m, source, branch, _ := branchFixture(t)
	branch.setValue("branch draft")
	source.state.Busy = true // Inspecting a running source is read-only.
	client := source.client
	msg := m.requestOrigin(branch)().(branchOpenMsg)
	if msg.err != nil || msg.entry != "old" {
		t.Fatalf("origin read: %+v", msg)
	}
	m.branchOpenResult(msg)
	if m.current() != source || len(m.tabs) != 2 || source.follow || source.originJump != nil {
		t.Fatal("source not reused/located")
	}
	if source.editor.Value() != "original draft" || branch.editor.Value() != "branch draft" || !source.state.Busy || source.client != client {
		t.Fatal("navigation mutated source or drafts")
	}
	if !strings.Contains(source.toast.text, "已定位") {
		t.Fatalf("not located: %+v", source.toast)
	}
	// A name change is reflected without using that name as the relation key.
	source.state.Name = "Renamed\nsource"
	if label := m.originTitle(branch); !strings.Contains(label, "Renamed source") || strings.Contains(label, "\n") {
		t.Fatal(label)
	}
}

func TestOriginRejectsStaleMissingMismatchAndSymlink(t *testing.T) {
	for _, mode := range []string{"switch", "revision", "owner-closed", "missing", "mismatch", "symlink", "reserved", "no-session"} {
		t.Run(mode, func(t *testing.T) {
			m, source, branch, _ := branchFixture(t)
			if mode == "no-session" {
				m.opts.NoSession = true
				if m.requestOrigin(branch) != nil {
					t.Fatal("no-session navigation allowed")
				}
				return
			}
			cmd := m.requestOrigin(branch)
			switch mode {
			case "missing":
				if err := os.Remove(source.state.File); err != nil {
					t.Fatal(err)
				}
			case "mismatch":
				if err := os.WriteFile(source.state.File, []byte(`{"type":"session","id":"replacement"}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := source.state.File + ".saved"
				if err := os.Rename(source.state.File, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, source.state.File); err != nil {
					t.Fatal(err)
				}
			}
			msg := cmd().(branchOpenMsg)
			switch mode {
			case "switch":
				m.activate(0)
			case "revision":
				branch.revision++
			case "owner-closed":
				m.tabs = m.tabs[:1]
				m.active = 0
			case "reserved":
				source.mutation = &sessionAction{}
			}
			before := m.current()
			n := len(m.tabs)
			if cmd := m.branchOpenResult(msg); cmd != nil || len(m.tabs) != n || m.current() != before {
				t.Fatal("invalid navigation moved a tab")
			}
		})
	}
}

func TestOriginReusesAnAlreadyOpenPathAlias(t *testing.T) {
	m, source, branch, _ := branchFixture(t)
	alias := filepath.Join(t.TempDir(), "alias.jsonl")
	if err := os.Symlink(source.state.File, alias); err != nil {
		t.Fatal(err)
	}
	source.state.File = alias
	m.branchOpenResult(m.requestOrigin(branch)().(branchOpenMsg))
	if m.current() != source || len(m.tabs) != 2 || !strings.Contains(source.toast.text, "已定位") {
		t.Fatal("alias opened a duplicate writer or lost location")
	}
}

func TestOriginMissingActiveEntryDoesNotNavigateTree(t *testing.T) {
	m, source, branch, _ := branchFixture(t)
	source.state.Items = nil
	m.branchOpenResult(m.requestOrigin(branch)().(branchOpenMsg))
	if m.current() != source || !strings.Contains(source.toast.text, "不在当前活动分支") || source.originJump != nil {
		t.Fatalf("missing entry not safely handled: %+v", source.toast)
	}
}

func TestOriginOpensThenLocatesOnlyMatchingSnapshot(t *testing.T) {
	for _, mode := range []string{"success", "switched", "mismatch", "stale"} {
		t.Run(mode, func(t *testing.T) {
			m, source, branch, snapshot := branchFixture(t)
			path, id := source.state.File, source.state.ID
			m.tabs = []*tab{branch}
			m.active = 0
			msg := m.requestOrigin(branch)().(branchOpenMsg)
			m.branchOpenResult(msg)
			target := m.current()
			if target == branch || target.state.File != path || target.originJump == nil || len(m.tabs) != 2 {
				t.Fatal("new source tab missing deferred jump")
			}
			if mode == "switched" {
				m.activate(0)
			}
			if mode == "mismatch" {
				id = "replacement"
			}
			revision := target.revision
			if mode == "stale" {
				target.revision++
			}
			state, _ := json.Marshal(map[string]any{"sessionId": id, "sessionFile": path})
			m.Update(snapshotMsg{id: target.id, revision: revision, state: state, entries: snapshot})
			switch mode {
			case "success":
				if target.originJump != nil || target.follow || !strings.Contains(target.toast.text, "已定位") {
					t.Fatal("not located after load")
				}
			case "switched":
				if m.current() != branch || !target.follow || target.originJump != nil {
					t.Fatal("background late load changed navigation")
				}
			case "mismatch":
				if !strings.Contains(target.toast.text, "身份已变化") {
					t.Fatal("identity mismatch not rejected")
				}
			case "stale":
				if target.originJump == nil || !target.follow {
					t.Fatal("stale snapshot drove scroll")
				}
			}
		})
	}
}

func TestOriginRefreshRestoresHeaderAndCommands(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	client, _ := fakeClient(t)
	tab.client = client
	path := filepath.Join(t.TempDir(), "reopened.jsonl")
	h := session.Header{Type: "session", ID: "fake", ParentSession: filepath.Join(t.TempDir(), "parent.jsonl"), Fork: &session.ForkProvenance{Version: 1, SourceID: "parent", UserEntryID: "u", CutoffEntryID: "a", Turn: 2}}
	raw, _ := json.Marshal(h)
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	tab.state.File = path
	msg := m.refresh(tab)().(snapshotMsg)
	if msg.err != nil || msg.origin == nil || msg.origin.Fork == nil {
		t.Fatalf("refresh omitted header: %+v", msg)
	}
	// The generic fake RPC omits sessionFile; a saved-session refresh supplies it.
	msg.state, _ = json.Marshal(map[string]any{"sessionId": "fake", "sessionFile": path})
	m.Update(msg)
	if m.chatTop(tab) != 1 || !strings.Contains(m.originTitle(tab), "第 2 轮") {
		t.Fatal("reopened source banner missing")
	}
	for _, text := range []string{"/origin"} {
		if cmd, ok := m.slash(tab, text); !ok || cmd == nil {
			t.Fatalf("missing local command %s", text)
		}
	}
}

func TestOriginBannerGeometryCopyAndSidebarClick(t *testing.T) {
	m, _, branch, _ := branchFixture(t)
	branch.follow = false
	m.document(branch)
	before := branch.doc.Text
	if m.chatTop(branch) != 1 || !m.originHit(2, 0) || m.pointerShapeAt(2, 0) != "pointer" {
		t.Fatal("banner not interactive")
	}
	if cmd := m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: 0}); cmd == nil {
		t.Fatal("banner click did not request origin")
	}
	m.beginSelection(branch, tea.Mouse{Button: tea.MouseLeft, X: 2, Y: 0})
	if branch.selection.dragging {
		t.Fatal("banner entered text selection")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape}) // dismiss the pending source read
	for _, b := range branch.doc.Blocks {
		if b.Role == "user" {
			if _, r, ok := m.forkTarget(2, b.Start+1); !ok || r.y != b.Start+1 {
				t.Fatal("fork button did not account for source banner")
			}
			break
		}
	}
	branch.doc = render.Document{Text: "copy me", Content: []render.ContentSpan{{Start: 0, End: 7, Content: true}}}
	branch.offset = 0
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: 1})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: 1})
	if copied := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: 1})); copied != "copy" {
		t.Fatalf("banner shifted copied text: %q", copied)
	}
	branch.selection = transcriptSelection{}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {32, 16}, {8, 8}} {
		m.width, m.height = size[0], size[1]
		m.resizeEditor(branch)
		m.document(branch)
		view := strings.Split(m.View().Content, "\n")
		if len(view) > m.height {
			t.Fatalf("view overflow at %v: %d", size, len(view))
		}
		for _, row := range view {
			if ansi.StringWidth(row) > m.width {
				t.Fatalf("width overflow at %v", size)
			}
		}
	}
	if strings.Contains(branch.doc.Text, "查看来源") || strings.Contains(before, "来自") {
		t.Fatal("source decoration contaminated copyable document")
	}
	m.width, m.height = 120, 40
	m.resizeEditor(branch)
	if strings.Contains(ansi.Strip(m.View().Content), "关联分支") {
		t.Fatal("sidebar still contains the removed branch entry")
	}
}

func TestOriginReadsCanBeCancelled(t *testing.T) {
	for _, cancel := range []string{"escape", "confirm"} {
		t.Run(cancel, func(t *testing.T) {
			m, source, branch, _ := branchFixture(t)
			source.offset = 9
			cmd := m.requestOrigin(branch)
			if cmd == nil || len(branch.dialogs) != 1 || branch.dialogs[0].kind != "branch-loading" || popupFilter(branch.dialogs[0]) {
				t.Fatal("read has no cancellable loading state")
			}
			if cancel == "escape" {
				m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
			} else {
				m.confirmDialog(branch, branch.dialogs[0])
			}
			// Execute the read only after cancellation: no sleeps or RPC.
			m.branchOpenResult(cmd().(branchOpenMsg))
			if m.current() != branch || len(m.tabs) != 2 || len(branch.dialogs) != 0 || len(source.dialogs) != 0 || source.offset != 9 || source.originJump != nil {
				t.Fatal("cancelled read reopened, navigated or scrolled")
			}
		})
	}
}

func TestOriginReadLoadingCleanup(t *testing.T) {
	for _, event := range []string{"success", "error", "revision", "switch", "repeat", "other-dialog"} {
		t.Run(event, func(t *testing.T) {
			m, _, branch, _ := branchFixture(t)
			cmd := m.requestOrigin(branch)
			loading := branch.dialogs[0]
			var keep *dialog
			switch event {
			case "revision":
				branch.revision++
			case "switch":
				m.activate(0)
			case "repeat":
				m.requestOrigin(branch)
				keep = branch.dialogs[0]
			case "other-dialog":
				keep = m.newDialog("help", "unrelated", "")
				branch.dialogs = append(branch.dialogs, keep)
			}
			v := cmd().(branchOpenMsg)
			if event == "error" {
				v.err = os.ErrNotExist
			}
			m.branchOpenResult(v)
			for _, d := range branch.dialogs {
				if d == loading {
					t.Fatal("finished or stale read left loading dialog")
				}
			}
			if keep != nil && (len(branch.dialogs) != 1 || branch.dialogs[0] != keep) {
				t.Fatal("old result removed another dialog")
			}
			if event != "success" && event != "switch" && m.current() != branch {
				t.Fatal("invalid result changed active tab")
			}
		})
	}
}

func TestOriginDeferredJumpCanBeCancelled(t *testing.T) {
	m, source, branch, _ := branchFixture(t)
	source.refreshing = true
	source.state.Items = nil
	v := m.requestOrigin(branch)().(branchOpenMsg)
	m.branchOpenResult(v)
	if m.current() != source || source.originJump == nil || len(source.dialogs) != 1 {
		t.Fatal("waiting for history has no cancellable loading state")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	source.offset = 7
	m.finishOriginJump(source)
	if source.originJump != nil || len(source.dialogs) != 0 || source.offset != 7 {
		t.Fatal("cancelled deferred jump scrolled after history arrived")
	}
}
