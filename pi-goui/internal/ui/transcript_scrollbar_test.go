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

func TestTranscriptScrollbarOverlaysAndRestoresContent(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	text := strings.Repeat("x", 78) + "界"
	tab.doc = render.Document{Text: text}
	if got := ansi.Strip(m.transcriptViewport(tab, text, 100, 1)); got != text {
		t.Fatal("inactive scrollbar reserved space")
	}
	tab.noteScroll()
	got := ansi.Strip(m.transcriptViewport(tab, text, 100, 1))
	if got != strings.Repeat("x", 78)+" ┃" || ansi.StringWidth(got) != 80 {
		t.Fatalf("wide character overlay broken: %q", got)
	}
	if tab.doc.Text != text {
		t.Fatal("overlay mutated selectable document")
	}
	tab.scrollbarUntil = time.Now().Add(-time.Second)
	if got := ansi.Strip(m.transcriptViewport(tab, text, 100, 1)); got != text {
		t.Fatal("hidden scrollbar did not restore content")
	}
}

func TestTranscriptScrollbarGeometry(t *testing.T) {
	for _, tc := range []struct{ total, height, offset, start, size int }{
		{5, 10, 0, 0, 0}, {10, 10, 0, 0, 0}, {100, 10, 0, 0, 1}, {100, 10, 45, 4, 1}, {100, 10, 90, 9, 1},
		{20, 10, 10, 5, 5}, {100, 1, 99, 0, 1}, {100, 10, 999, 9, 1}, {100, 10, -5, 0, 1},
	} {
		start, size := transcriptThumb(tc.total, tc.height, tc.offset)
		if start != tc.start || size != tc.size {
			t.Fatalf("%+v: got %d,%d", tc, start, size)
		}
	}
}

func TestTranscriptScrollbarViewAndMouse(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	tab.doc = render.Document{Text: strings.Repeat("message\n", 99) + "last"}
	tab.dirty, tab.follow, tab.offset = false, false, 0
	height := m.transcriptHeight(tab)
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasSuffix(rows[0], " ") {
		t.Fatal("scrollbar visible before scrolling")
	}
	m.scroll(tab, 1)
	m.scroll(tab, -1)
	raw := strings.Split(m.View().Content, "\n")
	if !strings.Contains(raw[0], render.ScrollbarMark("┃")) {
		t.Fatal("scrollbar color differs from RECENTS")
	}
	rows = strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasSuffix(rows[0], "┃") || !strings.HasSuffix(rows[height-1], "│") {
		t.Fatal("missing scrollbar at top")
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 80 {
			t.Fatalf("view width changed: %q", row)
		}
	}
	m.scroll(tab, 1000)
	rows = strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasSuffix(rows[height-1], "┃") {
		t.Fatal("scrollbar did not reach bottom")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth(), Y: 0})
	if tab.selection.dragging {
		t.Fatal("click outside viewport started text selection")
	}
	tab.scrollbarUntil = time.Now().Add(-time.Second)
	rows = strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasSuffix(rows[height-1], " ") {
		t.Fatal("scrollbar remained visible after scrolling stopped")
	}
	tab.doc = render.Document{Text: "short"}
	tab.offset = 0
	rows = strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasSuffix(rows[0], " ") {
		t.Fatal("unnecessary scrollbar for short content")
	}
}

func TestTranscriptScrollbarDoesNotReserveWidth(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	tab.state.Items = []session.Item{{ID: "a", Role: "assistant", Text: strings.Repeat("wide text ", 100)}}
	m.document(tab)
	for _, row := range strings.Split(tab.doc.Text, "\n") {
		if ansi.StringWidth(row) > m.mainWidth() {
			t.Fatal("message exceeds viewport width")
		}
	}
	if !strings.Contains(ansi.Strip(tab.doc.Text), "╮") {
		t.Fatal("message frame lost right border")
	}
}
