package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	frames "pi-goui/internal/render/frame"
)

func TestInputWheelScrollAndScrollbar(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	tab.setValue("first\n" + strings.Repeat("middle\n", 18) + "last")
	m.resizeEditor(tab)
	if tab.editor.Area.Height() != 5 {
		t.Fatalf("height=%d", tab.editor.Area.Height())
	}
	before := tab.offset
	m.Update(tea.MouseWheelMsg{X: 4, Y: m.editorTop(tab) + 2, Button: tea.MouseWheelUp})
	offset, total, browsing := tab.editor.ScrollPosition()
	if !browsing || total != 20 || offset != 12 {
		t.Fatalf("offset=%d total=%d browsing=%v", offset, total, browsing)
	}
	if tab.offset != before {
		t.Fatal("scroll changed transcript")
	}
	if tab.editor.Area.Line() != 16 || tab.editor.Area.Column() != len("middle") {
		t.Fatalf("caret not at last visible line end: %d:%d", tab.editor.Area.Line(), tab.editor.Area.Column())
	}
	if cursor := m.inputCursor(tab); cursor == nil || cursor.Y != m.editorTop(tab)+5 {
		t.Fatalf("missing or misplaced scroll caret: %+v", cursor)
	}
	rows := strings.Split(m.editorFrame(tab), "\n")
	thumb := false
	for i, line := range rows {
		if strings.HasSuffix(ansi.Strip(line), "┃") {
			thumb = true
			if i < 1 || i > 5 {
				t.Fatalf("scrollbar outside text at row %d", i)
			}
		}
	}
	if !thumb {
		t.Fatal("missing scrollbar")
	}
	for i := 0; i < 10; i++ {
		tab.editor.Scroll(-3)
	}
	if !strings.Contains(tab.editor.View(), "first") {
		t.Fatal("cannot reach first line")
	}
	for i := 0; i < 10; i++ {
		tab.editor.Scroll(3)
	}
	if !strings.Contains(tab.editor.View(), "last") {
		t.Fatal("cannot reach last line")
	}
	press(m, 'x', 0, "x")
	if !strings.HasSuffix(tab.editor.Value(), "lastx") {
		t.Fatal("typing did not insert at the scrolled caret")
	}
	if _, _, browsing := tab.editor.ScrollPosition(); browsing {
		t.Fatal("typing should restore caret view")
	}
}

func TestInputScrollbarPreservesFrameColors(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	tab.setValue(strings.Repeat("line\n", 10))
	m.resizeEditor(tab)
	rows := strings.Split(m.editorFrame(tab), "\n")
	width := m.mainWidth()
	base := frames.Box("input", make([]string, len(rows)-2), width, m.cfg.Theme)
	for i, row := range rows {
		got := strings.ReplaceAll(ansi.Cut(row, width-1, width), "┃", "│")
		want := ansi.Cut(base[i], width-1, width)
		// ansi.Cut retains earlier style sequences; compare the final edge color.
		got = got[strings.LastIndex(got, "\x1b[38;2;"):]
		want = want[strings.LastIndex(want, "\x1b[38;2;"):]
		if got != want {
			t.Fatalf("right border color changed at row %d: got %q want %q", i, got, want)
		}
	}
}

func TestInputScrollbarPersistsOnOverflow(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	check := func(want bool) {
		t.Helper()
		for i := 0; i < 2; i++ {
			if got := strings.Contains(ansi.Strip(m.editorFrame(tab)), "┃"); got != want {
				t.Fatalf("scrollbar visible=%v, want %v", got, want)
			}
		}
	}
	tab.setValue(strings.Repeat("line\n", 5) + "last")
	m.resizeEditor(tab)
	check(true) // Visible before the first wheel event.
	press(m, 'x', 0, "x")
	check(true) // Editing does not hide it.
	tab.editor.Scroll(-3)
	check(true)
	tab.setValue(strings.Repeat("line\n", 4) + "last")
	m.resizeEditor(tab)
	check(false) // Exactly five rows need no scrollbar.
	tab.setValue(strings.Repeat("word ", 100))
	m.resizeEditor(tab)
	check(true) // Soft wrapping also counts as overflow.
	tab.setValue("short")
	m.resizeEditor(tab)
	check(false)
}

func TestInputSoftWrapGrowsToFiveRows(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	tab := m.current()
	tab.setValue(strings.Repeat("word ", 100))
	m.resizeEditor(tab)
	if tab.editor.Area.Height() != 5 {
		t.Fatalf("height=%d", tab.editor.Area.Height())
	}
	for _, delta := range []int{-3, -3, 3} {
		tab.editor.Scroll(delta)
		cursor := m.inputCursor(tab)
		line := tab.editor.Area.LineInfo()
		if cursor == nil || cursor.Y != m.editorTop(tab)+5 {
			t.Fatalf("soft-wrap caret not on last visible row: %+v", cursor)
		}
		if line.RowOffset < line.Height-1 && line.ColumnOffset != line.Width-1 {
			t.Fatalf("soft-wrap caret not at row end: %+v", line)
		}
	}
	tab.setValue("short")
	m.resizeEditor(tab)
	if tab.editor.Area.Height() != 1 {
		t.Fatal("input did not shrink")
	}
}
