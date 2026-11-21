package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func TestMouseFoldWhileSameWidthLayoutInFlight(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	for i := range 30 {
		tab.state.Items = append(tab.state.Items, session.Item{ID: fmt.Sprintf("u%d", i), Role: "user", Text: "question"}, session.Item{ID: fmt.Sprintf("t%d", i), Role: "tool", ToolName: "read", Text: "output\nnext"})
	}
	m.document(tab)
	tab.invalidateStream()
	pending := m.scheduleDocument(tab)
	if pending == nil {
		t.Fatal("no pending same-width layout")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- pending() }()
	// Scroll a middle group to the top (not the last tool), then follow the
	// actual press/release path as a streaming job uses its own cache copy.
	tab.follow = false
	for _, b := range tab.doc.Blocks {
		if b.ID == "tool-group:t13" {
			tab.offset = b.Start
			break
		}
	}
	pos := tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop}
	m.mouse(pos)
	m.mouseRelease(pos)
	if !tab.folds["tool-group:t13"] {
		t.Fatal("group failed to expand")
	}
	want := render.Transcript(tab.state.Items, m.cfg.Theme, m.documentOptions(tab))
	m.Update(<-result)
	if !equalDocuments(tab.doc, want) {
		t.Fatal("same-width pending layout clobbered fold or changed block geometry")
	}
}
