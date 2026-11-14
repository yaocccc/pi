package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

func testModel(t *testing.T, keys string) *Model {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	if keys != "" {
		if err := os.WriteFile(filepath.Join(root, "keybindings.json"), []byte(keys), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Options{Cwd: root, Bin: filepath.Join(root, "not-real-pi")}, cfg)
	t.Cleanup(func() { _ = m.Close() })
	m.document(m.current())
	return m
}
func press(m *Model, code rune, mod tea.KeyMod, text string) {
	m.Update(tea.KeyPressMsg{Code: code, Mod: mod, Text: text})
}
func event(m *Model, t *tab, raw string) {
	m.Update(eventMsg{id: t.id, event: rpc.Event{Data: json.RawMessage(raw)}})
}

// Tests deliver the same prepared payload shape as the asynchronous producer.
func prepareSnapshotMessage(tab *tab, message snapshotMsg) snapshotMsg {
	message.client, message.file, message.sourceID = tab.client, tab.state.File, tab.state.ID
	message.prepared, message.prepareErr = tab.state.SnapshotPreparation().Prepare(message.state, message.entries, message.stats)
	message.preparationDone = true
	return message
}

func prepareForkMessage(t testing.TB, id int, fork session.Fork) forkCreatedMsg {
	t.Helper()
	state, _ := json.Marshal(map[string]any{"sessionId": fork.ID, "sessionFile": fork.Path, "sessionName": fork.Name, "cwd": fork.Cwd})
	var base session.State
	prepared, err := base.SnapshotPreparation().Prepare(state, fork.Entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	return forkCreatedMsg{id: id, fork: fork, prepared: prepared}
}

func assertValue(t *testing.T, tab *tab, want string) {
	t.Helper()
	if got := tab.editor.Value(); got != want {
		t.Fatalf("draft = %q, want %q", got, want)
	}
}

func TestNativeConfigDispatch(t *testing.T) {
	m := testModel(t, `{"app.session.resume":["ctrl+y"],"app.session.new":[],"app.thinking.toggle":["alt+t"],"app.tools.expand":[],"tui.editor.cursorLineStart":[]}`)
	tab := m.current()
	tab.setValue("draft")
	cmd := m.key(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("configured resume did not dispatch")
	}
	if _, ok := cmd().(sessionsMsg); !ok {
		t.Fatal("Ctrl+Y fell through to editor yank")
	}
	assertValue(t, tab, "draft")
	original := tab.showThinking
	press(m, 't', tea.ModCtrl, "")
	if tab.showThinking != original {
		t.Fatal("replaced Ctrl+T default still active")
	}
	press(m, 't', tea.ModAlt, "")
	if tab.showThinking == original {
		t.Fatal("remapped Alt+T not active")
	}
	press(m, 'o', tea.ModCtrl, "")
	if tab.expanded {
		t.Fatal("disabled expand default still active")
	}
	press(m, 'a', tea.ModCtrl, "")
	press(m, 'x', 0, "x")
	assertValue(t, tab, "draftx")
	press(m, 'n', tea.ModCtrl, "")
	if len(m.tabs) != 1 || m.current() != tab {
		t.Fatal("disabled Ctrl+N new-session binding still active")
	}
	assertValue(t, tab, "draftx")
	tab.setValue("")
	press(m, tea.KeyEnter, tea.ModShift, "")
	assertValue(t, tab, "\n")
	if got := config.KeyChord(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift}); !m.cfg.Keys.Match("app.model.cycleBackward", got) {
		t.Fatalf("enhanced reverse-model key = %q", got)
	}
}

func TestStaleSnapshotCannotRegressLiveState(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	if err := tab.state.LoadState(json.RawMessage(`{"sessionId":"one","model":{"id":"current"}}`)); err != nil {
		t.Fatal(err)
	}
	revision := tab.revision
	event(m, tab, `{"type":"agent_start"}`)
	event(m, tab, `{"type":"message_start","message":{"role":"assistant","timestamp":1,"content":[{"type":"text","text":"new live text"}]}}`)
	m.Update(prepareSnapshotMessage(tab, snapshotMsg{id: tab.id, revision: revision, state: json.RawMessage(`{"sessionId":"old","model":{"id":"stale"},"isStreaming":false}`), entries: json.RawMessage(`{"entries":[],"leafId":null}`)}))
	if !tab.state.Busy || tab.state.ID != "one" || tab.state.Model.ID != "current" {
		t.Fatalf("stale snapshot regressed state: %+v", tab.state)
	}
	if len(tab.state.Items) == 0 || tab.state.Items[0].Text != "new live text" {
		t.Fatal("stale history erased live message")
	}
	busyRevision := tab.revision
	event(m, tab, `{"type":"agent_settled"}`)
	m.Update(prepareSnapshotMessage(tab, snapshotMsg{id: tab.id, revision: busyRevision, state: json.RawMessage(`{"sessionId":"one","isStreaming":true}`)}))
	if tab.state.Busy {
		t.Fatal("old busy snapshot resurrected a settled run")
	}
}
func TestPartialSnapshotRetainsHistory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	entries := json.RawMessage(`{"entries":[{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":"before compaction"}},{"type":"compaction","id":"c1","parentId":"u1","summary":"summary","firstKeptEntryId":"u1"},{"type":"message","id":"a1","parentId":"c1","message":{"role":"assistant","content":"after compaction"}}],"leafId":"a1"}`)
	m.Update(prepareSnapshotMessage(tab, snapshotMsg{id: tab.id, revision: tab.revision, state: json.RawMessage(`{"sessionId":"one","thinkingLevel":"high"}`), entries: entries, err: errors.New("optional commands unavailable")}))
	text := ansi.Strip(tab.doc.Text)
	if !strings.Contains(text, "before compaction") || !strings.Contains(text, "after compaction") {
		t.Fatalf("valid history discarded after optional failure: %s", text)
	}
	if !strings.Contains(tab.status, "optional commands unavailable") {
		t.Fatal("refresh error not surfaced")
	}
}
func TestStreamingRenderCoalescedAndBackgroundScoped(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.setValue("first draft")
	m.document(first)
	doc := first.doc.Text
	second := m.addTab("")
	second.setValue("second draft")
	m.document(second)
	event(m, first, `{"type":"agent_start"}`)
	event(m, first, `{"type":"message_start","message":{"role":"assistant","timestamp":42,"content":"background text"}}`)
	if first.doc.Text != doc || !first.dirty {
		t.Fatal("background event rendered immediately")
	}
	if second.state.Busy {
		t.Fatal("busy leaked into active session")
	}
	assertValue(t, second, "second draft")
	m.activate(0)
	m.Update(tickMsg(time.Now()))
	if !strings.Contains(ansi.Strip(first.doc.Text), "background text") {
		t.Fatal("switch did not render latest background message")
	}
	prior := first.doc.Text
	event(m, first, `{"type":"message_update","message":{"role":"assistant","timestamp":42,"content":"stream updated"}}`)
	if first.doc.Text != prior {
		t.Fatal("streaming event eagerly rendered Markdown")
	}
	m.Update(tickMsg(time.Now()))
	finishStreamingDocument(m)
	if !strings.Contains(ansi.Strip(first.doc.Text), "stream updated") {
		t.Fatal("tick did not flush streaming render")
	}
	assertValue(t, first, "first draft")
	assertValue(t, second, "second draft")
	m.reply(first, replyMsg{op: "prompt", data: json.RawMessage(`{"disposition":"queued"}`)})
	if !first.state.Busy {
		t.Fatal("prompt acknowledgement cleared Busy")
	}
}
func TestDraftFailureRestorationAndHistory(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	image := map[string]any{"type": "image", "data": "AA==", "mimeType": "image/png"}
	tab.setValue("")
	reply := replyMsg{op: "prompt", text: " original \n text ", images: []map[string]any{image}, draftRevision: tab.draftRevision, err: errors.New("rejected")}
	m.reply(tab, reply)
	assertValue(t, tab, reply.text)
	if len(tab.images) != 1 || len(tab.history) != 0 {
		t.Fatal("failure lost image or polluted history")
	}
	tab.setValue("")
	reply.draftRevision = tab.draftRevision
	press(m, 'a', 0, "a")
	press(m, tea.KeyBackspace, 0, "")
	assertValue(t, tab, "")
	tab.images = nil
	m.reply(tab, reply)
	assertValue(t, tab, "")
	if len(tab.images) != 0 {
		t.Fatal("failure contaminated an edited draft with old image")
	}
	reply.err = nil
	m.reply(tab, reply)
	m.reply(tab, reply)
	if len(tab.history) != 1 {
		t.Fatalf("duplicate successful history entries: %v", tab.history)
	}
}
func TestClipboardSearchAndDialogIsolation(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.setValue("first")
	second := m.addTab("")
	second.setValue("second")
	m.Update(clipboardImageMsg{id: first.id, err: errors.New("no image")})
	m.Update(tea.ClipboardMsg{Content: " clip"})
	assertValue(t, first, "first clip")
	assertValue(t, second, "second")
	m.searching = "find"
	m.Update(tea.PasteMsg{Content: "needle"})
	if m.search != "needle" {
		t.Fatal("search paste not routed to search")
	}
	assertValue(t, second, "second")
	m.searching = ""
	d := m.newDialog("editor", "Edit", "")
	d.editor.SetValue("base")
	second.dialogs = []*dialog{d}
	press(m, tea.KeyEnter, tea.ModShift, "")
	press(m, 'x', 0, "x")
	if d.editor.Value() != "base\nx" {
		t.Fatalf("modal newline = %q", d.editor.Value())
	}
	assertValue(t, second, "second")
	press(m, tea.KeyEscape, 0, "")
	if len(second.dialogs) != 0 {
		t.Fatal("modal Escape failed")
	}
	assertValue(t, second, "second")
	second.dialogs = []*dialog{m.newDialog("confirm", "Confirm", "")}
	second.dialogs[0].options = []option{{Label: "No", Value: "no"}, {Label: "Yes", Value: "yes"}}
	m.Update(tea.PasteMsg{Content: "bad filter"})
	if len(m.filtered(second.dialogs[0])) != 2 {
		t.Fatal("paste hid confirmation choices")
	}
}
func TestScopedDialogKeysAndTitles(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	second := m.addTab("")
	m.extension(first, json.RawMessage(`{"method":"setTitle","title":"first title"}`))
	m.extension(second, json.RawMessage(`{"method":"setTitle","title":"second title"}`))
	if m.View().WindowTitle != "second title" {
		t.Fatal("active title wrong")
	}
	m.activate(0)
	if m.View().WindowTitle != "first title" {
		t.Fatal("background title lost or leaked")
	}
	d := m.newDialog("session", "Sessions", "")
	first.dialogs = []*dialog{d}
	press(m, 'n', tea.ModCtrl, "")
	if !d.namedOnly || len(m.tabs) != 2 {
		t.Fatal("Ctrl+N not scoped to session filter")
	}
	first.dialogs = []*dialog{m.newDialog("tree", "Tree", "")}
	press(m, 'o', tea.ModCtrl, "")
	if first.dialogs[0].message != "" || len(m.cfg.Keys.Keys("app.tree.filter.cycleForward")) != 0 {
		t.Fatal("removed tree binding remains active")
	}
}
func TestViewportBoundsAndFoldOffsets(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.state.Items = []session.Item{{ID: "u/0", Role: "user", Text: "Unicode 界界 👩‍💻 long message"}, {ID: "tool:x", Role: "tool", ToolID: "x", ToolName: "read", Text: strings.Repeat("tool output\n", 20)}, {ID: "a/0", Role: "assistant", Text: "**Answer**\n\n```go\nfmt.Println(\"hello\")\n```"}}
	for _, size := range [][2]int{{132, 36}, {80, 24}, {60, 18}, {32, 9}, {8, 3}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		lines := strings.Split(view.Content, "\n")
		if len(lines) != size[1] {
			t.Errorf("%v height = %d", size, len(lines))
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > size[0] {
				t.Errorf("%v row %d width = %d: %q", size, i, w, line)
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	tab.follow = false
	tab.offset = 0
	var block render.Block
	for _, b := range tab.doc.Blocks {
		if b.Foldable {
			block = b
			break
		}
	}
	if block.ID == "" {
		t.Fatal("missing foldable tool")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 4, Y: transcriptTop + block.Start})
	if tab.folds[block.ID] {
		t.Fatal("mouse down must not fold before a possible drag")
	}
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 4, Y: transcriptTop + block.Start})
	if !tab.folds[block.ID] {
		t.Fatal("tool hit-test didn't fold correct stable ID")
	}
	d := m.newDialog("model", "Models", strings.Repeat("wide 界", 40))
	d.options = []option{{Label: strings.Repeat("name", 50), Value: "m"}}
	tab.dialogs = []*dialog{d}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	for _, line := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(line) > 60 {
			t.Fatal("dialog overflow")
		}
	}
}
func TestOpenSessionAliasDoesNotCreateSecondWriter(t *testing.T) {
	m := testModel(t, "")
	path := filepath.Join(m.opts.Cwd, "saved.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(m.opts.Cwd, "alias.jsonl")
	if err := os.Symlink(path, alias); err != nil {
		t.Skip(err)
	}
	m.current().state.File = path
	m.open(alias)
	if len(m.tabs) != 1 {
		t.Fatal("symlink opened a second session writer")
	}
}
