package input

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
)

const (
	piUndoLimit = 128
	piUndoBytes = 4 << 20
	piKillLimit = 32
	piKillBytes = 1 << 20
)

type piEditorSnapshot struct {
	text     string
	row, col int
}

// piEditorState belongs to this local editor, not to the RPC agent. All mutation is
// synchronous in Update. External draft replacement starts a fresh undo group.
type piEditorState struct {
	undo         []piEditorSnapshot
	kills        []string // newest first
	last         piEditorSnapshot
	seen, typing bool
	lastKill     bool
	jump         int
	yankBase     piEditorSnapshot
	yankIndex    int
	yankValid    bool
}

func (m *Editor) snapshot() piEditorSnapshot {
	row, col := m.Area.Line(), m.Area.Column()
	return piEditorSnapshot{m.Area.Value(), row, col}
}

func (m *Editor) piRestoreEditor(s piEditorSnapshot) {
	m.Area.SetValue(s.text)
	keys := m.Area.KeyMap
	m.Area.KeyMap = textarea.DefaultKeyMap()
	m.Area, _ = m.Area.Update(tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	// CursorDown operates on visual rows. Starting at each logical line's
	// end skips its soft wraps, so this loop advances exactly one logical row.
	for m.Area.Line() < s.row && m.Area.Line() < m.Area.LineCount()-1 {
		m.Area.CursorEnd()
		m.Area.CursorDown()
	}
	m.Area.SetCursorColumn(s.col)
	m.Area.KeyMap = keys
}

func (s *piEditorState) pushUndo(snapshot piEditorSnapshot) {
	if len(snapshot.text) > piUndoBytes {
		// Never jump across an edit whose pre-image could not be retained.
		s.undo = nil
		return
	}
	s.undo = append(s.undo, snapshot)
	total := 0
	start := len(s.undo)
	for start > 0 && len(s.undo)-start < piUndoLimit {
		if total+len(s.undo[start-1].text) > piUndoBytes {
			break
		}
		start--
		total += len(s.undo[start].text)
	}
	if start > 0 {
		s.undo = append([]piEditorSnapshot(nil), s.undo[start:]...)
	}
}

func (s *piEditorState) kill(text string, backward bool) {
	if text == "" {
		return
	}
	if s.lastKill && len(s.kills) > 0 {
		if backward {
			text += s.kills[0]
		} else {
			text = s.kills[0] + text
		}
		s.kills = s.kills[1:]
	}
	if len(text) > piKillBytes {
		s.lastKill = false
		return
	}
	s.kills = append([]string{text}, s.kills...)
	total, n := 0, 0
	for n < len(s.kills) && n < piKillLimit && total+len(s.kills[n]) <= piKillBytes {
		total += len(s.kills[n])
		n++
	}
	s.kills = append([]string(nil), s.kills[:n]...)
	s.lastKill = true
}

func piRemovedText(before, after string) string {
	a, b := []rune(before), []rune(after)
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	j, k := len(a), len(b)
	for j > i && k > i && a[j-1] == b[k-1] {
		j--
		k--
	}
	return string(a[i:j])
}

func piEditorOffset(s piEditorSnapshot) int {
	lines := strings.Split(s.text, "\n")
	offset := 0
	for i := 0; i < s.row && i < len(lines); i++ {
		offset += len([]rune(lines[i])) + 1
	}
	return offset + s.col
}

func piSnapshotAt(text string, offset int) piEditorSnapshot {
	r := []rune(text)
	offset = max(0, min(offset, len(r)))
	prefix := string(r[:offset])
	row := strings.Count(prefix, "\n")
	col := utf8.RuneCountInString(prefix[strings.LastIndex(prefix, "\n")+1:])
	return piEditorSnapshot{text, row, col}
}

// piCharEdit handles grapheme clusters, including combining accents and emoji,
// so editing does not split a combining sequence or joined emoji.
func (m *Editor) piCharEdit(action string, before piEditorSnapshot) {
	r := []rune(before.text)
	pos := piEditorOffset(before)
	left, right := 0, len(r)
	g := uniseg.NewGraphemes(before.text)
	end := 0
	for g.Next() {
		start := end
		end += utf8.RuneCountInString(g.Str())
		if start < pos {
			left = start
		}
		if end > pos {
			right = end
			break
		}
	}
	if strings.HasSuffix(action, "Left") || strings.HasSuffix(action, "Backward") {
		if pos == 0 {
			return
		}
		if strings.Contains(action, "delete") {
			r = append(r[:left], r[pos:]...)
			m.piRestoreEditor(piSnapshotAt(string(r), left))
		} else {
			m.piRestoreEditor(piSnapshotAt(before.text, left))
		}
	} else {
		if pos >= len(r) {
			return
		}
		if strings.Contains(action, "delete") {
			r = append(r[:pos], r[right:]...)
			m.piRestoreEditor(piSnapshotAt(string(r), pos))
		} else {
			m.piRestoreEditor(piSnapshotAt(before.text, right))
		}
	}
}

// piWordParts follows pi-tui's word-navigation punctuation policy using UAX #29
// boundaries. Unlike Intl.Segmenter, uniseg has no ICU dictionary segmentation
// for languages such as Chinese/Thai; those word stops may differ, but complete
// grapheme clusters are always retained.
type piWordPart struct {
	text string
	kind int // 0 punctuation/symbols, 1 word-like, 2 whitespace
}

func piWordParts(text string) []piWordPart {
	const punctuation = "(){}[]<>.,;:'\"!?+-=*/\\|&%^$#@~`"
	var parts []piWordPart
	state := -1
	for text != "" {
		var word string
		word, text, state = uniseg.FirstWordInString(text, state)
		wordLike := strings.ContainsFunc(word, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) })
		g := uniseg.NewGraphemes(word)
		start := len(parts)
		for g.Next() {
			cluster := g.Str()
			r, _ := utf8.DecodeRuneInString(cluster)
			kind := 0
			if unicode.IsSpace(r) || r == '\ufeff' {
				kind = 2
			} else if wordLike && !strings.ContainsRune(punctuation, r) {
				kind = 1
			}
			if len(parts) > start && parts[len(parts)-1].kind == kind {
				parts[len(parts)-1].text += cluster
			} else {
				parts = append(parts, piWordPart{cluster, kind})
			}
		}
	}
	return parts
}

func piWordBoundary(text string, pos int, backward bool) int {
	r := []rune(text)
	pos = max(0, min(pos, len(r)))
	fragment := string(r[pos:])
	if backward {
		fragment = string(r[:pos])
	}
	parts := piWordParts(fragment)
	if backward {
		for len(parts) > 0 && parts[len(parts)-1].kind == 2 {
			pos -= utf8.RuneCountInString(parts[len(parts)-1].text)
			parts = parts[:len(parts)-1]
		}
		if len(parts) == 0 {
			return pos
		}
		if parts[len(parts)-1].kind == 1 {
			return pos - utf8.RuneCountInString(parts[len(parts)-1].text)
		}
		for len(parts) > 0 && parts[len(parts)-1].kind == 0 {
			pos -= utf8.RuneCountInString(parts[len(parts)-1].text)
			parts = parts[:len(parts)-1]
		}
	} else {
		for len(parts) > 0 && parts[0].kind == 2 {
			pos += utf8.RuneCountInString(parts[0].text)
			parts = parts[1:]
		}
		if len(parts) == 0 {
			return pos
		}
		if parts[0].kind == 1 {
			return pos + utf8.RuneCountInString(parts[0].text)
		}
		for len(parts) > 0 && parts[0].kind == 0 {
			pos += utf8.RuneCountInString(parts[0].text)
			parts = parts[1:]
		}
	}
	return pos
}

func (m *Editor) piWordEdit(action string, before piEditorSnapshot) {
	pos := piEditorOffset(before)
	backward := strings.HasSuffix(action, "Left") || strings.HasSuffix(action, "Backward")
	target := piWordBoundary(before.text, pos, backward)
	if strings.Contains(action, "delete") {
		r := []rune(before.text)
		lo, hi := min(pos, target), max(pos, target)
		m.piRestoreEditor(piSnapshotAt(string(r[:lo])+string(r[hi:]), lo))
	} else {
		m.piRestoreEditor(piSnapshotAt(before.text, target))
	}
}

// piEdit accepts semantic actions; the caller owns bindings and history-edge
// navigation. Unbound control/Alt keys never reach Bubbles' legacy key map.
func (m *Editor) Update(action string, msg tea.Msg) tea.Cmd {
	m.scrollBrowsing = false
	m.scrollRows = nil
	key, isKey := msg.(tea.KeyPressMsg)
	if isKey && key.Text == "" && key.Code >= 32 && key.Code <= utf8.MaxRune && key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta|tea.ModHyper) == 0 {
		key.Text = string(key.Code)
	}
	paste, isPaste := msg.(tea.PasteMsg)
	if !isKey && !isPaste {
		var cmd tea.Cmd
		m.Area, cmd = m.Area.Update(msg)
		return cmd
	}
	if isPaste {
		key = tea.KeyPressMsg{Text: paste.Content}
	}

	if isPaste {
		action = "" // pasted content is text, never a keybinding
	}
	before := m.snapshot()
	s := &m.state
	if s.seen && before.text != s.last.text {
		s.undo = nil
		s.typing = false
		s.yankValid = false
		s.lastKill = false
		s.jump = 0
	}
	contiguous := s.seen && before == s.last
	plain := isPaste || key.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta|tea.ModHyper) == 0 && key.Text != ""
	var cmd tea.Cmd
	defer func() {
		s.last = m.snapshot()
		s.seen = true
	}()

	if isPaste {
		s.jump = 0
	}
	if s.jump != 0 {
		direction := s.jump
		s.jump = 0
		s.typing = false
		s.yankValid = false
		s.lastKill = false
		if plain && !isPaste {
			target := []rune(key.Text)
			if len(target) > 0 {
				line := []rune(strings.Split(before.text, "\n")[before.row])
				for i := before.col + direction; i >= 0 && i < len(line); i += direction {
					if line[i] == target[0] {
						m.Area.SetCursorColumn(i)
						break
					}
				}
				if len(target) > 1 {
					return m.Update("", tea.KeyPressMsg{Text: string(target[1:])})
				}
				return nil
			}
		}
		// Escape and other non-character keys cancel the pending seek.
		return nil
	}
	if action == "tui.editor.jumpForward" || action == "tui.editor.jumpBackward" {
		s.jump = 1
		if action == "tui.editor.jumpBackward" {
			s.jump = -1
		}
		s.typing = false
		s.yankValid = false
		s.lastKill = false
		return nil
	}
	if action == "tui.editor.undo" {
		if n := len(s.undo); n > 0 {
			m.piRestoreEditor(s.undo[n-1])
			s.undo = s.undo[:n-1]
		}
		s.typing = false
		s.yankValid = false
		s.lastKill = false
		return nil
	}
	if action == "tui.editor.yank" || action == "tui.editor.yankPop" {
		s.typing = false
		s.lastKill = false
		if len(s.kills) == 0 {
			return nil
		}
		if action == "tui.editor.yankPop" {
			if !s.yankValid || !contiguous || len(s.kills) < 2 {
				return nil
			}
			s.pushUndo(before)
			s.yankIndex = (s.yankIndex + 1) % len(s.kills)
			m.piRestoreEditor(s.yankBase)
		} else {
			s.pushUndo(before)
			s.yankBase = before
			s.yankIndex = 0
		}
		m.Area.InsertString(s.kills[s.yankIndex])
		s.yankValid = true
		return nil
	}
	s.yankValid = false
	kill := strings.HasPrefix(action, "tui.editor.deleteWord") || strings.HasPrefix(action, "tui.editor.deleteToLine")
	if !kill {
		s.lastKill = false
	}
	typing := action == "" && plain && !isPaste && key.Text != ""
	backward := strings.HasSuffix(action, "Backward") || strings.HasSuffix(action, "Start")
	line := []rune(strings.Split(before.text, "\n")[before.row])
	boundary := backward && before.col == 0 || !backward && before.col == len(line)
	if kill && (strings.HasPrefix(action, "tui.editor.deleteToLine") || boundary) {
		r := []rune(before.text)
		pos := piEditorOffset(before)
		lo, hi := pos, pos
		if backward {
			lo = pos - before.col
			if lo == pos && pos > 0 {
				lo--
			}
		} else {
			hi = pos + len(line) - before.col
			if hi == pos && pos < len(r) {
				hi++
			}
		}
		text := string(r[:lo]) + string(r[hi:])
		m.piRestoreEditor(piSnapshotAt(text, lo))
	} else if action == "tui.editor.cursorLeft" || action == "tui.editor.cursorRight" || action == "tui.editor.deleteCharBackward" || action == "tui.editor.deleteCharForward" {
		m.piCharEdit(action, before)
	} else if strings.HasPrefix(action, "tui.editor.cursorWord") || strings.HasPrefix(action, "tui.editor.deleteWord") {
		m.piWordEdit(action, before)
	} else if action == "" {
		if plain {
			m.Area.InsertString(key.Text)
		}
	} else {
		mapped, ok := piEditorActionKey(action)
		if ok {
			keys := m.Area.KeyMap
			m.Area.KeyMap = textarea.DefaultKeyMap()
			m.Area, cmd = m.Area.Update(mapped)
			m.Area.KeyMap = keys
		}
	}
	after := m.snapshot()
	if after.text != before.text {
		if !typing || !s.typing || !contiguous {
			s.pushUndo(before)
		}
		if kill {
			s.kill(piRemovedText(before.text, after.text), strings.HasSuffix(action, "Backward") || strings.HasSuffix(action, "Start"))
		}
	}
	s.typing = typing
	return cmd
}

func piEditorActionKey(action string) (tea.KeyPressMsg, bool) {
	keys := map[string]tea.KeyPressMsg{
		"tui.editor.cursorUp":           {Code: tea.KeyUp},
		"tui.editor.cursorDown":         {Code: tea.KeyDown},
		"tui.editor.cursorWordLeft":     {Code: tea.KeyLeft, Mod: tea.ModAlt},
		"tui.editor.cursorWordRight":    {Code: tea.KeyRight, Mod: tea.ModAlt},
		"tui.editor.cursorLineStart":    {Code: tea.KeyHome},
		"tui.editor.cursorLineEnd":      {Code: tea.KeyEnd},
		"tui.editor.deleteWordBackward": {Code: 'w', Mod: tea.ModCtrl},
		"tui.editor.deleteWordForward":  {Code: 'd', Mod: tea.ModAlt},
		"tui.editor.deleteToLineStart":  {Code: 'u', Mod: tea.ModCtrl},
		"tui.editor.deleteToLineEnd":    {Code: 'k', Mod: tea.ModCtrl},
		"tui.input.newLine":             {Code: tea.KeyEnter},
	}
	key, ok := keys[action]
	return key, ok
}

type Editor struct {
	Area           textarea.Model
	state          piEditorState
	scrollRows     []string
	scrollOffset   int
	scrollBrowsing bool
}

func New() Editor {
	a := textarea.New()
	// Report the real caret through tea.View so terminal IMEs can anchor to it.
	a.SetVirtualCursor(false)
	a.KeyMap = textarea.KeyMap{}
	a.CharLimit = 0
	// MaxHeight also caps logical content in Bubbles unless explicitly disabled.
	// UI callers still limit the visible viewport with SetHeight.
	a.MaxHeight = 0
	a.ShowLineNumbers = false
	a.Prompt = ""
	return Editor{Area: a}
}

// Resize uses the textarea's own word/grapheme wrapping and viewport clamping.
// MaxHeight is enabled only during this layout pass: leaving it set in fixed-
// height mode would also cap editable content in Bubbles' legacy behavior.
func (m *Editor) Resize(width, heightLimit int) {
	m.scrollBrowsing = false
	m.scrollRows = nil
	dynamic, maximum := m.Area.DynamicHeight, m.Area.MaxHeight
	m.Area.DynamicHeight = true
	m.Area.MaxHeight = max(1, heightLimit)
	m.Area.SetWidth(max(1, width)) // recalculates visual height and clamps stale offsets
	// InsertString/SetValue can leave the viewport's content stale until Update.
	// A non-key update synchronizes it without invoking any editor binding.
	focused := m.Area.Focused()
	if !focused {
		m.Area.Focus()
	}
	m.Area, _ = m.Area.Update(nil)
	if !focused {
		m.Area.Blur()
	}
	m.Area.DynamicHeight, m.Area.MaxHeight = dynamic, maximum
}

func (m *Editor) Value() string { return m.Area.Value() }
func (m *Editor) SetValue(v string) {
	m.scrollBrowsing = false
	m.scrollRows = nil
	m.Area.SetValue(v)
	m.state = piEditorState{kills: m.state.kills}
}
func (m *Editor) Jumping() bool { return m.state.jump != 0 }
