package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
)

func TestMessageTargetPointerAndGesture(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tab.state.Items = []session.Item{{ID: "a", Role: "assistant", Text: "https://example.org\n\n" + file}}
	tab.dirty = true
	m.document(tab)
	for _, want := range []string{"https://example.org", file} {
		found := false
		for y := 0; y < m.transcriptHeight(tab) && !found; y++ {
			for x := 0; x < m.mainWidth(); x++ {
				raw, ok := tab.doc.MessageTargetAt(tab.offset+y, x)
				if !ok || raw != want {
					continue
				}
				found = true
				click := tea.Mouse{Button: tea.MouseLeft, X: x, Y: y + transcriptTop}
				if shape := m.pointerShapeAt(x, click.Y); shape != "pointer" {
					t.Fatalf("%s: pointer = %s", want, shape)
				}
				m.beginSelection(tab, click)
				if m.mouseRelease(click) == nil {
					t.Fatalf("%s: no click action", want)
				} // Do not launch a real browser or editor.
				m.beginSelection(tab, click)
				drag := click
				drag.X++
				m.mouseMotion(drag)
				if text := clipboardText(t, m.mouseRelease(drag)); text == "" {
					t.Fatal("drag did not copy")
				}
				break
			}
		}
		if !found {
			t.Fatalf("missing %s", want)
		}
	}
}
