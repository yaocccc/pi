package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
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
		if ansi.Cut(ansi.Strip(rows[box.y+1]), box.x, box.x+box.width) != "│ Copied"+strings.Repeat(" ", box.width-10)+" │" {
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
		if toastLifetime != 5*time.Second {
			t.Fatal("notification lifetime must be five seconds")
		}
		if _, ok = m.toastBox(first, now.Add(4999*time.Millisecond)); !ok {
			t.Fatal("toast disappeared before five seconds")
		}
		if box.width != 10 || ansi.Strip(box.rows[0]) != "╭────────╮" {
			t.Fatal("notification must retain the original compact frame")
		}
		assertNoBackground(t, strings.Join(box.rows, ""))
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
	if strings.ContainsAny(tab.toast.text, "\x1b\a") || tab.toast.text != "Copied\n中文"+strings.Repeat("x", 1000) {
		t.Fatalf("unsafe or truncated text %q", tab.toast.text)
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
func TestToastUsesAccentTextAndBorderWithoutBackground(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.cfg.Theme.Background = "#112233"
	m.cfg.Theme.Text = "#ddeeff"
	m.cfg.Theme.Accent = "#aabbcc"
	now := time.Now()
	m.current().notifyAt("Saved", now)
	box, ok := m.toastBox(m.current(), now)
	if !ok {
		t.Fatal("missing notification")
	}
	styled := strings.Join(box.rows, "\n")
	assertNoBackground(t, styled)
	if !strings.Contains(box.rows[1], m.paint("Saved", m.cfg.Theme.Accent)) {
		t.Fatal("notification text must use the accent color")
	}
	for _, row := range box.rows {
		if !strings.Contains(row, "38;2;170;187;204") {
			t.Fatal("notification border must use the accent color")
		}
	}
}

func TestToastMultilineWrapAndGeometry(t *testing.T) {
	for _, width := range []int{8, 10, 24, 60, 132} {
		m := testModel(t, "")
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		tab := m.current()
		now := time.Now()
		for _, message := range []string{"first\n\nlast", strings.Repeat("中文👩‍💻é", 8), strings.Repeat("longword", 12), "wrap words at the available notification width"} {
			tab.notifyAt(message, now)
			box, ok := m.toastBox(tab, now)
			if !ok || box.height <= 3 || box.height != len(box.rows) {
				t.Fatalf("width %d: missing multiline box: %+v", width, box)
			}
			if box.x < 0 || box.x+box.width >= m.mainWidth() || box.y+box.height > m.transcriptHeight(tab) || box.y+box.height >= m.editorTop(tab) {
				t.Fatalf("width %d: escaped chat: %+v", width, box)
			}
			for _, row := range box.rows {
				if ansi.StringWidth(row) != box.width {
					t.Fatalf("width %d: row width %d != %d: %q", width, ansi.StringWidth(row), box.width, row)
				}
			}
			if !box.contains(box.x, box.y+box.height-1) || box.contains(box.x, box.y+box.height) {
				t.Fatal("multiline hitbox differs from frame")
			}
			original := fitBox(strings.Repeat("x", m.mainWidth())+"\n", m.mainWidth(), m.height)
			for _, row := range strings.Split(m.overlayToast(tab, original, now), "\n") {
				if ansi.StringWidth(row) != m.mainWidth() {
					t.Fatal("overlay changed view width")
				}
			}
		}
	}
}

func TestToastPreservesLinesAndLongText(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	tab := m.current()
	now := time.Now()
	tab.notifyAt(" first  line\n\nlast\tline ", now)
	box, ok := m.toastBox(tab, now)
	if !ok || box.height != 5 {
		t.Fatalf("paragraphs not preserved: %+v", box)
	}
	for i, want := range []string{"first line", "", "last line"} {
		if got := strings.TrimSpace(ansi.Cut(ansi.Strip(box.rows[i+1]), 2, box.width-2)); got != want {
			t.Fatalf("line %d: %q != %q", i, got, want)
		}
	}
	message := strings.Repeat("x", 300) + "THE-END"
	tab.notifyAt(message, now)
	box, ok = m.toastBox(tab, now)
	var content strings.Builder
	for _, row := range box.rows[1 : len(box.rows)-1] {
		content.WriteString(strings.TrimSpace(ansi.Cut(ansi.Strip(row), 2, box.width-2)))
	}
	if !ok || content.String() != message {
		t.Fatal("text beyond the old 256-column limit was lost")
	}
	// Clicking a newly added row dismisses the notification, not the transcript.
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: box.x + 1, Y: box.y + box.height - 2})
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: box.x + 1, Y: box.y + box.height - 2})
	if tab.toast.text != "" || tab.selection.dragging {
		t.Fatal("multiline toast did not consume click")
	}
}

func TestToastHeightAndRetentionLimits(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 10})
	tab := m.current()
	now := time.Now()
	tab.notifyAt(strings.Repeat("中文🙂", 20000), now)
	if !utf8.ValidString(tab.toast.text) || len(tab.toast.text) > 64*1024+len("…") || !strings.HasSuffix(tab.toast.text, "…") {
		t.Fatal("retained text is not safely bounded")
	}
	box, ok := m.toastBox(tab, now)
	if !ok || box.height > m.transcriptHeight(tab) || box.y+box.height >= m.editorTop(tab) {
		t.Fatalf("height limit failed: %+v", box)
	}
	if !strings.Contains(ansi.Strip(box.rows[len(box.rows)-2]), "…") {
		t.Fatal("hidden text not marked with an ellipsis")
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
