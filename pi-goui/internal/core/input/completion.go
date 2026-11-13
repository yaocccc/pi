package input

// CursorOffset is a rune offset, independent of terminal cells and soft wraps.
func (m *Editor) CursorOffset() int { return piEditorOffset(m.snapshot()) }

// CompleteRange replaces a rune range as one undoable edit and keeps the cursor
// at cursor runes into the inserted text. It never interprets inserted key bytes.
func (m *Editor) CompleteRange(start, end int, text string, cursor int) {
	before := m.snapshot()
	r := []rune(before.text)
	start = max(0, min(start, len(r)))
	end = max(start, min(end, len(r)))
	inserted := []rune(text)
	cursor = max(0, min(cursor, len(inserted)))
	next := string(r[:start]) + text + string(r[end:])
	if next != before.text {
		m.state.pushUndo(before)
	}
	m.piRestoreEditor(piSnapshotAt(next, start+cursor))
	m.state.last = m.snapshot()
	m.state.seen = true
	m.state.typing = false
	m.state.yankValid = false
	m.state.lastKill = false
	m.state.jump = 0
}
