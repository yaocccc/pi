package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func TestMouseFramedSelectionSkipsBordersAndPadding(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	tab.doc = render.Transcript([]session.Item{{ID: "u", Role: "user", Text: "hello"}, {ID: "a", Role: "assistant", Text: "world"}}, m.cfg.Theme, render.Options{Width: 50})
	tab.dirty, tab.follow, tab.offset = false, false, 0
	rows := strings.Split(tab.doc.Text, "\n")
	m.beginSelection(tab, tea.Mouse{Button: tea.MouseLeft, X: 0, Y: transcriptTop})
	tab.selection.end = textPoint{len(rows) - 1, 49}
	tab.selection.moved = true
	if got := tab.selection.text(); got != "hello\nworld" {
		t.Fatalf("copied %q", got)
	}
	for i, span := range tab.doc.Content {
		marked := m.selectionLine(tab, rows[i], i)
		if ansi.Strip(marked) != ansi.Strip(rows[i]) {
			t.Fatal("highlight changed display text")
		}
		if !span.Content && marked != rows[i] {
			t.Fatal("frame decoration highlighted")
		}
		if span.Content {
			if !strings.HasPrefix(marked, ansi.Cut(rows[i], 0, span.Start)) {
				t.Fatal("leading frame/padding highlighted")
			}
		}
	}
	// The snapshot owns its spans even when a new document is assigned.
	tab.doc.Content = nil
	if got := tab.selection.text(); got != "hello\nworld" {
		t.Fatalf("snapshot lost spans: %q", got)
	}
}

func TestSelectionContentPreservesIndentationBlankLinesAndWideText(t *testing.T) {
	rows := []string{"╭──────────╮", "│   │x│    │", "│          │", "│   界é   │", "╰──────────╯"}
	s := transcriptSelection{anchor: textPoint{0, 0}, end: textPoint{4, 11}, moved: true, lines: rows, content: []render.ContentSpan{{}, {Start: 2, End: 7, Content: true}, {Start: 2, End: 2, Content: true}, {Start: 2, End: 7, Content: true}, {}}}
	if got := s.text(); got != "  │x│\n\n  界é" {
		t.Fatalf("copied %q", got)
	}
	s.anchor, s.end = textPoint{3, 5}, textPoint{3, 6}
	if got := s.text(); got != "界é" {
		t.Fatalf("wide selection %q", got)
	}
	s.anchor, s.end = s.end, s.anchor
	if got := s.text(); got != "界é" {
		t.Fatalf("reverse wide selection %q", got)
	}
}
