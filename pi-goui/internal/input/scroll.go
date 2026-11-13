package input

import (
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// Scroll positions the caret at the end of the last visible visual row without
// changing the draft or its undo history.
func (m *Editor) Scroll(delta int) {
	if delta == 0 {
		return
	}
	m.ensureScrollRows()
	m.scrollBrowsing = true
	m.scrollOffset = m.Area.ScrollYOffset()
	m.scrollOffset = min(max(0, m.scrollOffset+delta), max(0, len(m.scrollRows)-m.Area.Height()))
	target := min(len(m.scrollRows)-1, m.scrollOffset+m.Area.Height()-1)
	keys := m.Area.KeyMap
	m.Area.KeyMap = textarea.DefaultKeyMap()
	m.Area, _ = m.Area.Update(tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	m.Area.KeyMap = keys
	for i := 0; i < target; i++ {
		m.Area.CursorDown()
	}
	line := m.Area.LineInfo()
	end := line.StartColumn + line.Width
	if line.RowOffset < line.Height-1 {
		// A soft-wrap boundary belongs to the next row. Stay on this row.
		end = max(line.StartColumn, end-1)
	}
	m.Area.SetCursorColumn(end)
	m.Area, _ = m.Area.Update(nil)
	m.scrollOffset = m.Area.ScrollYOffset()
}

func (m *Editor) View() string {
	return m.Area.View()
}

func (m *Editor) ensureScrollRows() {
	if m.scrollRows != nil {
		return
	}
	preview := New()
	preview.Area.SetStyles(m.Area.Styles())
	preview.SetValue(m.Value())
	preview.Resize(m.Area.Width(), utf8.RuneCountInString(m.Value())+1)
	m.scrollRows = strings.Split(preview.Area.View(), "\n")
}

func (m *Editor) ScrollPosition() (offset, total int, browsing bool) {
	m.ensureScrollRows()
	return m.Area.ScrollYOffset(), len(m.scrollRows), m.scrollBrowsing
}
