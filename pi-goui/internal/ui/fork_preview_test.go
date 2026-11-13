package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func assertForkNotWritten(t *testing.T, m *Model, source *tab) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(filepath.Dir(source.state.File), "*.jsonl"))
	if err != nil || len(files) != 1 || len(m.tabs) != 1 || source.editor.Value() != "original draft" {
		t.Fatalf("request changed files/tabs/draft: %v %v", files, err)
	}
}

func TestForkDirectRejectsChangedSource(t *testing.T) {
	for _, scenario := range []string{"revision", "busy", "dead", "client", "identity", "path", "empty", "mutation", "reserved", "no-session", "rpc-error", "invalid-boundary"} {
		t.Run(scenario, func(t *testing.T) {
			m, source, _, snapshot := uiForkFixture(t)
			selected := "old"
			if scenario == "busy" || scenario == "revision" {
				selected = "new" // The latest round still needs idle and an unchanged revision.
			}
			v := m.requestFork(source, selected)().(forkSnapshotMsg)
			v.entries = snapshot
			path := source.state.File
			switch scenario {
			case "revision":
				source.revision++
			case "busy":
				source.state.Busy = true
			case "dead":
				source.dead = true
			case "client":
				source.client = nil
			case "identity":
				source.state.ID = "changed"
			case "path":
				source.state.File += ".changed"
			case "empty":
				source.forkPending = false
			case "mutation":
				source.mutation = &sessionAction{}
			case "reserved":
				m.sessionActions = map[string]*sessionAction{path: {path: path}}
			case "no-session":
				m.opts.NoSession = true
			case "rpc-error":
				v.err = errors.New("snapshot failed")
			case "invalid-boundary":
				v.selected = "answer"
			}
			if m.forkSnapshot(v) != nil {
				t.Fatal("unsafe snapshot scheduled a write")
			}
			source.state.File = path
			assertForkNotWritten(t, m, source)
			if source.forkPending || len(source.dialogs) != 0 {
				t.Fatal("failed request retained pending state or opened a dialog")
			}
		})
	}
}

func TestForkDirectPreservesOtherDialogAndReleasesWriteFailure(t *testing.T) {
	m, source, _, snapshot := uiForkFixture(t)
	other := m.newDialog("confirm", "Other request", "Keep this")
	source.dialogs = append(source.dialogs, other)
	v := m.requestFork(source, "old")().(forkSnapshotMsg)
	v.entries = snapshot
	write := m.forkSnapshot(v)
	if write == nil || len(source.dialogs) != 1 || source.dialogs[0] != other {
		t.Fatal("direct fork replaced another dialog")
	}
	if m.requestFork(source, "new") != nil {
		t.Fatal("duplicate request accepted while writing")
	}
	if err := os.Remove(source.state.File); err != nil {
		t.Fatal(err)
	}
	result := write().(forkCreatedMsg)
	if result.err == nil {
		t.Fatal("missing source unexpectedly created a fork")
	}
	m.forkCreated(result)
	if source.forkPending || len(m.tabs) != 1 || source.editor.Value() != "original draft" || source.dialogs[0] != other {
		t.Fatal("failed write changed source UI or retained pending state")
	}
}

func TestForkHoverClickAndCopyIsolation(t *testing.T) {
	m, source, _, snapshot := uiForkFixture(t)
	m.width, m.height = 120, 40
	m.document(source)
	source.follow, source.offset = false, 0
	before := source.doc.Text
	var rect hitRect
	found := false
	for _, b := range source.doc.Blocks {
		if b.ID != "old/0" {
			continue
		}
		id, target, ok := m.forkTarget(2, transcriptTop+b.Start)
		if !ok || id != "old" {
			t.Fatal("user header has no action")
		}
		rect, found = target, true
	}
	if !found {
		t.Fatal("missing user block")
	}
	for _, b := range source.doc.Blocks {
		if b.Role == "assistant" {
			if _, _, ok := m.forkTarget(2, transcriptTop+b.Start); ok {
				t.Fatal("assistant reply acquired a fork action")
			}
		}
	}
	m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, rect.x, rect.y
	rows := strings.Split(before, "\n")
	m.overlayFork(rows)
	if !strings.Contains(ansi.Strip(rows[rect.y-transcriptTop]), forkButton) || source.doc.Text != before {
		t.Fatal("missing overlay or polluted document")
	}
	if m.pointerShapeAt(rect.x, rect.y) != "pointer" {
		t.Fatal("button cursor missing")
	}
	source.selection.dragging = true
	if _, _, ok := m.forkTarget(rect.x, rect.y); ok {
		t.Fatal("button interferes with drag")
	}
	source.selection.dragging = false
	if _, _, ok := m.forkTarget(m.mainWidth()+1, rect.y); ok {
		t.Fatal("action leaked into side panel")
	}
	cmd := m.mouse(tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y})
	if cmd == nil || source.selection.dragging {
		t.Fatal("button click began selection instead of creating a fork")
	}
	v := cmd().(forkSnapshotMsg)
	v.entries = snapshot
	if v.selected != "old" {
		t.Fatal("clicked wrong message")
	}
	write := m.forkSnapshot(v)
	if write == nil || len(source.dialogs) != 0 {
		t.Fatal("click did not directly schedule creation")
	}
	assertForkNotWritten(t, m, source)
	result := write().(forkCreatedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(source.dialogs) != 0 || source.doc.Text != before {
		t.Fatal("direct fork changed dialogs or copy text")
	}
	m.forkCreated(result)
	if m.current() == source || m.current().state.Name != "[1]"+source.state.Name {
		t.Fatal("direct click did not open the independent numbered fork")
	}
}

func TestForkEmptyChoicesAndFullTextFilter(t *testing.T) {
	m, source, _, _ := uiForkFixture(t)
	d := m.newDialog("fork", "Choices", "")
	source.dialogs = append(source.dialogs, d)
	if m.confirmDialog(source, d) != nil || source.forkPending {
		t.Fatal("empty choices requested a fork")
	}
	text := strings.Repeat("long question ", 12) + "unique-tail"
	d.options = []option{forkChoice(source, "old", text)}
	d.editor.SetValue("unique-tail")
	if len(m.filtered(d)) != 1 {
		t.Fatal("truncated presentation broke full-text filtering")
	}
	assertForkNotWritten(t, m, source)
}

func TestForkDirectNarrowAndCandidateDetails(t *testing.T) {
	m, source, _, snapshot := uiForkFixture(t)
	o := forkChoice(source, "old", "first")
	if !strings.Contains(o.Label, "第 1 轮") || !strings.Contains(o.Detail, "earlier answer") {
		t.Fatalf("missing choice context: %+v", o)
	}
	m.width, m.height = 8, 8
	m.reply(source, replyMsg{op: "get_fork_messages", data: json.RawMessage(`{"messages":[{"entryId":"old","text":"first"}]}`)})
	cmd := m.confirmDialog(source, source.dialogs[0])
	if cmd == nil {
		t.Fatal("small window blocked selection")
	}
	v := cmd().(forkSnapshotMsg)
	v.entries = snapshot
	write := m.forkSnapshot(v)
	if write == nil || len(source.dialogs) != 0 {
		t.Fatal("small window required a preview")
	}
	result := write().(forkCreatedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.forkCreated(result)
	if len(m.tabs) != 2 || source.forkPending {
		t.Fatal("small window prevented direct creation")
	}
}
