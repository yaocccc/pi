package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func TestToastCopyPreservesWorkingAndDraft(t *testing.T) {
	m := testModel(t, "")
	tab := setDocument(m, "copy this")
	tab.status = "Ready"
	tab.setValue("draft")
	now := time.Now()
	tab.working.apply([]byte(`{"type":"agent_start"}`), now.Add(-time.Second))
	tab.working.apply([]byte(`{"type":"agent_settled"}`), now)
	before := m.workingLine(tab)
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: transcriptTop})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: transcriptTop})
	if text := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: 3, Y: transcriptTop})); text != "copy" {
		t.Fatal(text)
	}
	if tab.toast.text != "Copied" || tab.status != "Ready" || tab.editor.Value() != "draft" {
		t.Fatalf("copy modified unrelated state: %+v", tab.toast)
	}
	if got := m.workingLine(tab); got != before || strings.Contains(got, "Copied") || strings.Contains(got, "Ready") {
		t.Fatalf("working message changed: %q", got)
	}
	tab.state.Items = []session.Item{{Role: "assistant", Text: "answer"}}
	clipboardText(t, m.copyLast(tab))
	if tab.toast.text != "Copied" {
		t.Fatal(tab.toast.text)
	}
	tab.working = workingState{}
	if m.workingLine(tab) != "" {
		t.Fatal("Ready appeared while idle")
	}
}
func TestToastLayoutExpiryAndIsolation(t *testing.T) {
	m := testModel(t, "")
	now := time.Now()
	first := m.current()
	first.status = "Ready"
	for _, width := range []int{132, 80, 60} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		first.doc = render.Document{Text: strings.Repeat("transcript line\n", 80)}
		first.dirty = false
		first.offset = 7
		first.follow = false
		first.notifyAt("Copied", now)
		box, ok := m.toastBox(first, now)
		if !ok {
			t.Fatal("missing box")
		}
		original := fitBox(strings.Repeat("x", m.mainWidth())+"\n", m.mainWidth(), m.height)
		out := m.overlayToast(first, original, now)
		rows := strings.Split(out, "\n")
		if ansi.Cut(ansi.Strip(rows[box.y+1]), box.x, box.x+box.width) != "│ Copied │" {
			t.Fatalf("bad toast placement: %q", rows[box.y+1])
		}
		if box.x+box.width >= m.mainWidth() || box.y+box.height > m.transcriptHeight(first) {
			t.Fatal("toast escaped chat")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != m.mainWidth() {
				t.Fatal("changed view width")
			}
		}
		if first.offset != 7 || first.follow || first.dirty {
			t.Fatal("toast changed document/scroll")
		}
		if _, ok = m.toastBox(first, now.Add(toastLifetime)); ok {
			t.Fatal("toast did not expire")
		}
	}
	second := m.addTab("")
	first.notify("background notice")
	if _, ok := m.toastBox(second, time.Now()); ok {
		t.Fatal("toast leaked to another session")
	}
	m.activate(0)
	first.dialogs = []*dialog{m.newDialog("input", "Input", "")}
	if _, ok := m.toastBox(first, time.Now()); ok {
		t.Fatal("toast covers modal")
	}
}
func TestToastSafeTextAndDismissDoesNotFold(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := setDocument(m, strings.Repeat("underlying text\n", 40))
	now := time.Now()
	tab.notifyAt("Copied\n\x1b]52;c;EVIL\a\x1b[31m中文"+strings.Repeat("x", 1000), now)
	if strings.ContainsAny(tab.toast.text, "\x1b\n\a") || ansi.StringWidth(tab.toast.text) > 256 {
		t.Fatalf("unsafe/unbounded text %q", tab.toast.text)
	}
	tab.toast.text = "Copied"
	box, ok := m.toastBox(tab, now)
	if !ok {
		t.Fatal("missing box")
	}
	tab.doc.Blocks = []render.Block{{ID: "under", Role: "tool", Start: 0, End: 3, Foldable: true}}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: box.x + 1, Y: box.y + 1})
	cmd := m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: box.x + 1, Y: box.y + 1})
	if cmd != nil || tab.toast.text != "" || tab.folds["under"] || tab.selection.dragging {
		t.Fatal("toast click reached underlying transcript")
	}
	for _, size := range [][2]int{{1, 12}, {4, 12}, {60, 7}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		tab.notify("Copied")
		if _, ok := m.toastBox(tab, time.Now()); ok {
			t.Fatal("toast shown without space")
		}
	}
}
func TestToastNotificationsRemainSessionScoped(t *testing.T) {
	m := testModel(t, "")
	first := m.current()
	first.status = "persistent error"
	second := m.addTab("")
	m.extension(first, []byte(`{"type":"extension_ui_request","method":"notify","message":"Background notification"}`))
	if first.toast.text != "Background notification" || second.toast.text != "" || first.status != "persistent error" {
		t.Fatal("notification lost origin/error state")
	}
	m.Update(noticeMsg{id: first.id, text: "Saved setting"})
	if first.toast.text != "Saved setting" || second.toast.text != "" {
		t.Fatal("notice leaked")
	}
}
