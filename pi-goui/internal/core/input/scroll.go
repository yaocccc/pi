package input

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// Scroll positions the caret at the end of the last visible visual row without
// changing the draft or its undo history.
func (m *Editor) Scroll(delta int) {
	if delta == 0 {
		return
	}
	total := m.visualLineCount()
	m.scrollBrowsing = true
	m.scrollOffset = m.Area.ScrollYOffset()
	m.scrollOffset = min(max(0, m.scrollOffset+delta), max(0, total-m.Area.Height()))
	target := min(total-1, m.scrollOffset+m.Area.Height()-1)
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

func (m *Editor) visualLineCount() int {
	total := 0
	for line := range strings.SplitSeq(m.Value(), "\n") {
		total += wrappedLineCount(line, m.Area.Width())
	}
	return total
}

// wrappedLineCount mirrors Bubbles textarea's word wrapping, including its
// trailing caret row at exact-width boundaries. Keep only the current row and
// pending word: counting geometry must not create an editor or render its
// entire off-screen contents. Width is the textarea's actual content width,
// which already excludes the prompt, line-number gutter and frame padding.
func wrappedLineCount(text string, width int) int {
	width = max(1, width)
	rows := 1
	var row, word []rune
	for _, r := range text {
		if unicode.IsSpace(r) {
			if uniseg.StringWidth(string(row))+uniseg.StringWidth(string(word))+1 > width {
				rows++
				row = row[:0]
			}
			row = append(row, word...)
			row = append(row, ' ')
			word = word[:0]
			continue
		}
		word = append(word, r)
		if uniseg.StringWidth(string(word))+rw.RuneWidth(r) > width {
			if len(row) > 0 {
				rows++
				row = row[:0]
			}
			row = append(row, word...)
			word = word[:0]
		}
	}
	if uniseg.StringWidth(string(row))+uniseg.StringWidth(string(word)) >= width {
		rows++
	}
	return rows
}

func (m *Editor) ScrollPosition() (offset, total int, browsing bool) {
	return m.Area.ScrollYOffset(), m.visualLineCount(), m.scrollBrowsing
}
