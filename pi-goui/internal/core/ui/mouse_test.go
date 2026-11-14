package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func clipboardText(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("no OSC 52 clipboard command")
	}
	msg := cmd()
	if fmt.Sprintf("%T", msg) != "tea.setClipboardMsg" {
		t.Fatalf("not a clipboard message: %T", msg)
	}
	return fmt.Sprint(msg)
}
func setDocument(m *Model, text string) *tab {
	t := m.current()
	t.doc = render.Document{Text: text}
	t.dirty = false
	t.follow = false
	t.offset = 0
	return t
}
func TestMouseUnicodeSelectionAndConfiguredCopy(t *testing.T) {
	m := testModel(t, `{"tui.input.copy":"alt+c","app.message.copy":"alt+x"}`)
	tab := setDocument(m, "\x1b[31mA界e\u0301👩‍💻Z\x1b[0m\n  second line\n  third")
	tab.setValue("keep draft")
	// The anchor is the second cell of 界; the end is the second cell of the emoji.
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop})
	if got := tab.selection.text(); got != "界e\u0301👩‍💻" {
		t.Fatalf("unsafe Unicode selection %q", got)
	}
	if highlighted := m.selectionLine(tab, strings.Split(tab.doc.Text, "\n")[0], 0); ansi.Strip(highlighted) != "A界e\u0301👩‍💻Z" {
		t.Fatalf("highlight changed content: %q", highlighted)
	}
	if got := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop})); got != "界e\u0301👩‍💻" {
		t.Fatalf("clipboard %q", got)
	}
	assertValue(t, tab, "keep draft")
	if got := clipboardText(t, m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModAlt})); got != "界e\u0301👩‍💻" {
		t.Fatal(got)
	}
	assertValue(t, tab, "keep draft")
	if got := clipboardText(t, m.key(tea.KeyPressMsg{Code: 'x', Mod: tea.ModAlt})); got != "界e\u0301👩‍💻" {
		t.Fatal(got)
	}
	// Reverse selection across lines preserves indentation and strips ANSI.
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 6, Y: transcriptTop + 1})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: transcriptTop})
	copied := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: transcriptTop}))
	if copied != "界e\u0301👩‍💻Z\n  secon" {
		t.Fatalf("multiline selection %q", copied)
	}
	if strings.Contains(copied, "\x1b") {
		t.Fatal("clipboard contains terminal control")
	}
}
func TestMouseSelectionDoesNotFoldAndClickUsesEffectiveState(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.state.Items = []session.Item{{ID: "tool:1", Role: "tool", ToolName: "read", Text: "first output\nsecond output"}, {ID: "tool:2", Role: "tool", ToolName: "bash", Text: "done"}}
	m.document(tab)
	tab.follow = false
	tab.offset = 0
	find := func(id string) render.Block {
		t.Helper()
		for _, b := range tab.doc.Blocks {
			if b.ID == id {
				return b
			}
		}
		t.Fatalf("missing block %s", id)
		return render.Block{}
	}
	group := find("tool-group:tool:1")
	if group.Expanded || group.End-group.Start != 1 {
		t.Fatal("group must start as a collapsed single-line header")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + group.Start})
	if tab.folds[group.ID] {
		t.Fatal("folded on mouse down")
	}
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + group.Start})
	if !tab.folds[group.ID] {
		t.Fatal("group did not expand")
	}
	child := find("tool:1")
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + child.Start})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 7, Y: transcriptTop + child.Start})
	clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 7, Y: transcriptTop + child.Start}))
	if tab.folds[child.ID] {
		t.Fatal("drag toggled a child fold")
	}
	// Global expansion is effective even when no per-ID entry exists.
	tab.selection = transcriptSelection{}
	tab.folds = map[string]bool{}
	tab.expanded = true
	m.document(tab)
	child = find("tool:1")
	if !child.Expanded {
		t.Fatal("renderer omitted effective state")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: transcriptTop + child.Start})
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: transcriptTop + child.Start})
	if value, ok := tab.folds[child.ID]; !ok || value {
		t.Fatal("first click after global expansion must collapse child")
	}
}
func TestMouseDispatchAndBlankRows(t *testing.T) {
	m := testModel(t, "")
	tab := setDocument(m, "copy this")
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: transcriptTop})
	m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: 3, Y: transcriptTop})
	_, cmd := m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 3, Y: transcriptTop})
	if got := clipboardText(t, cmd); got != "copy" {
		t.Fatal(got)
	}
	tab.doc.Blocks = []render.Block{{ID: "last", Role: "tool", Start: 0, End: 1, Foldable: true}}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: transcriptTop + 3})
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 1, Y: transcriptTop + 3})
	if tab.folds["last"] {
		t.Fatal("blank space folded the last message")
	}
	// A terminal-managed Shift drag must not start an application selection.
	m.mouse(tea.Mouse{Button: tea.MouseLeft, Mod: tea.ModShift, X: 0, Y: transcriptTop})
	if tab.selection.dragging {
		t.Fatal("captured Shift drag")
	}
}

func TestExpandedToolOutputSelection(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.state.Items = []session.Item{{ID: "tool:1", Role: "tool", ToolName: "bash", Text: "unique output 你好\n  indented code"}}
	tab.expanded = true
	m.document(tab)
	tab.follow = false
	tab.offset = 0
	lines := strings.Split(tab.doc.Text, "\n")
	row := -1
	start := 0
	for i, line := range lines {
		plain := ansi.Strip(line)
		if byteIndex := strings.Index(plain, "unique output"); byteIndex >= 0 {
			row = i
			start = ansi.StringWidth(plain[:byteIndex])
			break
		}
	}
	if row < 0 {
		t.Fatal("missing expanded output")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: start, Y: row + transcriptTop})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: start + 5, Y: row + transcriptTop})
	if got := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: start + 5, Y: row + transcriptTop})); got != "unique" {
		t.Fatal(got)
	}
}
func TestSelectionSessionResizeAndStreamIsolation(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.state.Items = []session.Item{{ID: "a", Role: "assistant", Text: "original response"}}
	m.document(first)
	first.follow = false
	first.offset = 0
	old := first.doc.Text
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + 1})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop + 1})
	event(m, first, `{"type":"agent_start"}`)
	if !first.state.Busy {
		t.Fatal("selection blocked lifecycle projection")
	}
	first.state.Items = append(first.state.Items, session.Item{ID: "later", Role: "assistant", Text: "new live text"})
	m.Update(tickMsg(time.Now()))
	if first.doc.Text != old || !first.dirty {
		t.Fatal("drag snapshot was rebuilt or invalidation lost")
	}
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop + 1})
	m.Update(tickMsg(time.Now()))
	finishStreamingDocument(m)
	if first.doc.Text == old || first.dirty || first.selection.document != "" {
		t.Fatal("live text was not released after selection ended")
	}
	// Another session must never inherit either a completed or unfinished drag.
	first.follow = false
	first.offset = 0
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + 1})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop + 1})
	second := m.addTab("")
	if first.selection.dragging || second.selection.document != "" {
		t.Fatal("drag leaked across session activation")
	}
	if cmd := m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop + 1}); cmd != nil {
		t.Fatal("release copied a different session")
	}
	m.activate(0)
	first.follow = false
	first.offset = 0
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + 1})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 5, Y: transcriptTop + 1})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	if first.selection.document != "" {
		t.Fatal("resize kept stale cell coordinates")
	}
}
func TestLatestButtonLayoutClickAndFollowing(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	for _, size := range [][2]int{{132, 36}, {80, 24}, {60, 18}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		tab.state.Items = []session.Item{{ID: "a", Role: "assistant", Text: strings.Repeat("long transcript\n\n", 60)}}
		tab.widgets["test"] = []string{"widget one", "widget two"}
		tab.setValue("/")
		m.completionHidden = false
		m.resizeEditor(tab)
		m.document(tab)
		tab.follow = false
		tab.offset = 0
		label, rect, ok := m.latestButton(tab)
		if !ok {
			t.Fatalf("%v missing latest button", size)
		}
		lines := strings.Split(m.View().Content, "\n")
		if got := ansi.Cut(ansi.Strip(lines[rect.y]), rect.x, rect.x+rect.width); got != label {
			t.Fatalf("%v hitbox mismatch: %q vs %q at %#v", size, got, label, rect)
		}
		m.mouse(tea.Mouse{Button: tea.MouseLeft, X: rect.x + 1, Y: rect.y})
		if !tab.follow {
			t.Fatal("click did not restore follow")
		}
		if _, _, ok := m.latestButton(tab); ok {
			t.Fatal("latest button remained at bottom")
		}
		old := tab.offset
		tab.state.Items = append(tab.state.Items, session.Item{ID: "b", Role: "assistant", Text: "new stream line\n\nnext line"})
		tab.dirty = true
		m.document(tab)
		if tab.offset <= old {
			t.Fatal("new content did not auto follow")
		}
		m.scroll(tab, -5)
		if _, _, ok := m.latestButton(tab); !ok {
			t.Fatal("scroll did not expose latest button")
		}
		press(m, tea.KeyEnd, tea.ModCtrl, "")
		if !tab.follow {
			t.Fatal("native Ctrl+End behavior changed")
		}
	}
}
func TestTransparentNormalViewAndModal(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.cfg.Theme.Background = "#112233"
	m.cfg.Theme.AssistantBackground = "#334455"
	m.cfg.Theme.UserBackground = "#556677"
	tab.state.Items = []session.Item{{ID: "u", Role: "user", Text: "question `inline`"}, {ID: "a", Role: "assistant", Text: "**answer**\n\n```go\nfmt.Println(42)\n```"}}
	tab.setValue("draft")
	m.resizeEditor(tab)
	m.document(tab)
	assertNoBackground(t, m.View().Content)
	for _, kind := range []string{"input", "editor", "model"} {
		d := m.newDialog(kind, "Dialog", "message")
		d.editor.SetValue("modal input")
		tab.dialogs = []*dialog{d}
		assertNoBackground(t, m.View().Content)
	}
}
func assertNoBackground(t *testing.T, text string) {
	t.Helper()
	// Only the selected two-row session card may paint the workbench background.
	selected := "\x1b[48;2;82;85;88m"
	if strings.Count(text, selected) > 2 {
		t.Fatal("selected-session background escaped its two rows")
	}
	text = strings.ReplaceAll(text, selected, "")
	re := regexp.MustCompile("\\x1b\\[([0-9;:]+)m")
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		parts := strings.FieldsFunc(match[1], func(r rune) bool { return r == ';' || r == ':' })
		for i := 0; i < len(parts); i++ {
			code, _ := strconv.Atoi(parts[i])
			if code == 48 || (code >= 40 && code <= 47) || (code >= 100 && code <= 107) {
				t.Fatalf("explicit background in normal view: %q", match[0])
			}
			if code == 38 && i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 5 {
					i += 2
				} else if mode == 2 {
					i += 4
				}
			}
		}
	}
}
