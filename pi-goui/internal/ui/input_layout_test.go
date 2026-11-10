package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestInputInitialNewlineKeepsFirstLine(t *testing.T) {
	for _, tc := range []struct {
		name, keys string
		code       rune
		mod        tea.KeyMod
	}{
		{"ctrl-j", "", 'j', tea.ModCtrl},
		{"shift-enter", "", tea.KeyEnter, tea.ModShift},
		{"remapped-ctrl-n", `{"tui.input.newLine":"ctrl+n"}`, 'n', tea.ModCtrl},
		{"remapped-alt-j", `{"tui.input.newLine":"alt+j"}`, 'j', tea.ModAlt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testModel(t, tc.keys)
			m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
			tab := m.current()
			tab.setValue("first line")
			m.resizeEditor(tab)
			if tab.editor.Area.Height() != 1 {
				t.Fatal("initial height must be one")
			}
			press(m, tc.code, tc.mod, "")
			if tab.editor.Value() != "first line\n" {
				t.Fatalf("newline not dispatched: %q", tab.editor.Value())
			}
			if tab.editor.Area.Height() != 2 || tab.editor.Area.ScrollYOffset() != 0 {
				t.Fatalf("height=%d offset=%d; first line must stay visible", tab.editor.Area.Height(), tab.editor.Area.ScrollYOffset())
			}
			if !strings.Contains(ansi.Strip(tab.editor.Area.View()), "first line") {
				t.Fatal("first line scrolled out after growing")
			}
			if tab.editor.Area.Line() != 1 || tab.editor.Area.Column() != 0 {
				t.Fatal("cursor not on new line")
			}
			press(m, tea.KeyBackspace, 0, "")
			if tab.editor.Value() != "first line" || tab.editor.Area.Height() != 1 || tab.editor.Area.ScrollYOffset() != 0 {
				t.Fatal("backspace did not shrink to an unscrolled single line")
			}
		})
	}
}
func TestInputFooterAlwaysHasSpacer(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tab := m.current()
	for i := 0; i < 12; i++ {
		press(m, 'x', 0, "x")
		rows := strings.Split(m.editorFrame(tab), "\n")
		spacer := ansi.Strip(rows[len(rows)-3])
		if strings.Trim(spacer, "│ ") != "" {
			t.Fatalf("line %d: missing footer spacer: %q", i, spacer)
		}
		if !strings.Contains(ansi.Strip(rows[len(rows)-4]), "x") {
			t.Fatalf("line %d: spacer not immediately below input", i)
		}
		press(m, 'j', tea.ModCtrl, "")
		if m.editorFrameHeight(tab) != tab.editor.Area.Height()+4 {
			t.Fatal("newline consumed footer spacer")
		}
	}
}

func TestInputFillsFrameInterior(t *testing.T) {
	for _, width := range []int{132, 100, 80, 60} {
		m := testModel(t, "")
		m.Update(tea.WindowSizeMsg{Width: width, Height: 36})
		tab := m.current()
		want := m.mainWidth() - 4
		if tab.editor.Area.Width() != want {
			t.Fatalf("terminal=%d area=%d want=%d", width, tab.editor.Area.Width(), want)
		}
		lines := strings.Split(m.View().Content, "\n")
		for row := m.editorTop(tab); row < m.height; row++ {
			main := ansi.Strip(ansi.Cut(lines[row], 0, m.mainWidth()))
			left, right := "│", "│"
			if row == m.editorTop(tab) {
				left, right = "┌", "┐"
			}
			if row == m.height-1 {
				left, right = "└", "┘"
			}
			if ansi.StringWidth(main) != m.mainWidth() || !strings.HasPrefix(main, left) || !strings.HasSuffix(main, right) {
				t.Fatalf("terminal=%d frame row %d not aligned: %q", width, row, main)
			}
		}
	}
}
