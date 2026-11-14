package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
	"pi-goui/internal/core/render"
)

type textPoint struct{ line, cell int }

// document is an immutable display snapshot, not the asynchronously updated
// session history. A gesture freezes rendering only, never RPC consumption.
type transcriptSelection struct {
	anchor, end     textPoint
	dragging, moved bool
	document        string
	lines           []string
	content         []render.ContentSpan
}

func (s transcriptSelection) ordered() (textPoint, textPoint) {
	a, b := s.anchor, s.end
	if a.line > b.line || (a.line == b.line && a.cell > b.cell) {
		a, b = b, a
	}
	return a, b
}

// A terminal mouse position can land on the second cell of a wide grapheme.
// Snap both selection ends outward so clipboard text never contains half of it.
func cellSpan(line string, cell int) (int, int) {
	at := 0
	g := uniseg.NewGraphemes(ansi.Strip(line))
	for g.Next() {
		width := ansi.StringWidth(g.Str())
		if cell < at+width {
			return at, at + width
		}
		at += width
	}
	return at, at
}
func (s transcriptSelection) bounds(line string, row int) (int, int, bool) {
	if !s.moved {
		return 0, 0, false
	}
	a, b := s.ordered()
	if row < a.line || row > b.line {
		return 0, 0, false
	}
	lo, hi := 0, ansi.StringWidth(line)
	if row == a.line {
		lo, _ = cellSpan(line, a.cell)
	}
	if row == b.line {
		_, hi = cellSpan(line, b.cell)
	}
	if row < len(s.content) {
		span := s.content[row]
		if !span.Content {
			return 0, 0, false
		}
		lo, hi = max(lo, span.Start), min(hi, span.End)
	}
	return lo, hi, hi > lo
}
func (s transcriptSelection) text() string {
	if !s.moved {
		return ""
	}
	lines := s.lines
	if lines == nil {
		lines = strings.Split(s.document, "\n")
	}
	a, b := s.ordered()
	var selected []string
	for row := max(0, a.line); row <= b.line && row < len(lines); row++ {
		if row < len(s.content) && !s.content[row].Content {
			continue
		}
		line := ansi.Strip(lines[row])
		lo, hi, ok := s.bounds(line, row)
		part := ""
		if ok {
			part = ansi.Cut(line, lo, hi)
		}
		// Screen padding is not meaningful clipboard content. Preserve leading
		// spaces (e.g. code indentation) and empty intervening lines.
		selected = append(selected, strings.TrimRight(part, " "))
	}
	return render.Clean(strings.Join(selected, "\n"))
}
func (m *Model) selectionLine(t *tab, line string, row int) string {
	if t.selection.document != t.doc.Text {
		return line
	}
	lo, hi, ok := t.selection.bounds(line, row)
	if !ok {
		return line
	}
	selected := lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Color(m.cfg.Theme.Text)).Render(ansi.Cut(ansi.Strip(line), lo, hi))
	return ansi.Cut(line, 0, lo) + selected + ansi.Cut(line, hi, ansi.StringWidth(line))
}
func copySelection(t *tab, text string) tea.Cmd {
	t.selection.dragging = false
	t.notify("Copied")
	return tea.SetClipboard(text)
}
func (m *Model) transcriptPoint(t *tab, x, y int) textPoint {
	row := t.offset + min(max(0, y-m.chatTop(m.current())), max(0, m.transcriptHeight(t)-1))
	lineCount := len(t.selection.lines)
	if lineCount == 0 || t.selection.document != t.doc.Text {
		lineCount = len(t.documentLines())
	}
	row = min(max(0, row), lineCount-1)
	return textPoint{row, min(max(0, x), max(0, m.mainWidth()-1))}
}
func (m *Model) beginSelection(t *tab, v tea.Mouse) {
	m.inspector.selection = transcriptSelection{}
	t.selection = transcriptSelection{}
	if m.height < 8 || v.Mod&tea.ModShift != 0 || v.X < 0 || v.X >= m.mainWidth() || v.Y < m.chatTop(m.current()) || v.Y >= m.transcriptHeight(t)+m.chatTop(m.current()) {
		return
	}
	lines := t.documentLines()
	if v.Y-m.chatTop(m.current())+t.offset >= len(lines) {
		return // Blank viewport rows are not the last message's fold target.
	}
	point := m.transcriptPoint(t, v.X, v.Y)
	t.selection = transcriptSelection{anchor: point, end: point, dragging: true, document: t.doc.Text, lines: lines, content: t.doc.Content}
}
func (m *Model) mouseMotion(v tea.Mouse) {
	if m.movePanelResize(v) {
		return
	}
	if m.inspectorMotion(v) {
		return
	}
	t := m.current()
	if t == nil || !t.selection.dragging {
		return
	}
	if len(t.dialogs) > 0 {
		t.selection = transcriptSelection{}
		return
	}
	if v.Button != tea.MouseLeft {
		return
	}
	if v.Y < m.chatTop(m.current()) {
		m.scroll(t, -1)
	} else if v.Y >= m.transcriptHeight(t)+m.chatTop(m.current()) {
		m.scroll(t, 1)
	}
	point := m.transcriptPoint(t, v.X, v.Y)
	if point != t.selection.anchor {
		t.selection.moved = true
	}
	t.selection.end = point
	if t.selection.moved {
		t.follow = false
	}
}
func (m *Model) mouseRelease(v tea.Mouse) tea.Cmd {
	if m.panelResize.dragging {
		m.movePanelResize(tea.Mouse{X: v.X, Y: v.Y, Button: tea.MouseLeft})
		m.finishPanelResize()
		return nil
	}
	if cmd, ok := m.inspectorRelease(v); ok {
		return cmd
	}
	t := m.current()
	if t == nil || !t.selection.dragging {
		return nil
	}
	if v.Button != tea.MouseLeft && v.Button != tea.MouseNone {
		return nil
	}
	if len(t.dialogs) > 0 {
		t.selection = transcriptSelection{}
		return nil
	}
	point := m.transcriptPoint(t, v.X, v.Y)
	if point != t.selection.anchor {
		t.selection.moved = true
	}
	t.selection.end = point
	t.selection.dragging = false
	if t.selection.moved {
		t.follow = false
		if text := t.selection.text(); text != "" {
			return copySelection(t, text)
		}
		return nil
	}
	// Only a completed click may fold. Starting a drag on a tool row must
	// never alter its layout before the selected text has been copied.
	row := point.line
	t.selection = transcriptSelection{}
	if v.X < 0 || v.X >= m.mainWidth() || v.Y < m.chatTop(m.current()) || v.Y >= m.transcriptHeight(t)+m.chatTop(m.current()) {
		return nil
	}
	for _, link := range t.doc.ImageLinks {
		if row == link.Row && v.X >= link.Start && v.X < link.End {
			for _, item := range t.state.Items {
				if item.ID == link.ItemID && link.ImageIndex >= 0 && link.ImageIndex < len(item.Images) {
					return openImagePreview(m.imagePreviews, t.id, item.Images[link.ImageIndex])
				}
			}
			return nil
		}
	}
	if toggleDocumentBlock(t.doc, row, t.folds) {
		t.invalidateDocument()
		m.document(t)
	}
	return nil
}

type hitRect struct{ x, y, width int }

func (r hitRect) contains(x, y int) bool { return y == r.y && x >= r.x && x < r.x+r.width }

// Both drawing and hit-testing derive from this exact status-row layout.
func (m *Model) latestButton(t *tab) (string, hitRect, bool) {
	bottom := max(0, len(t.documentLines())-m.transcriptHeight(t))
	if m.height < 8 || len(t.dialogs) > 0 || t.follow || t.offset >= bottom {
		return "", hitRect{}, false
	}
	y := m.editorTop(t) - 1
	if y >= m.height-1 {
		return "", hitRect{}, false
	}
	label := "[↓ Latest]"
	if m.mainWidth() < ansi.StringWidth(label)+2 {
		label = "↓"
	}
	width := ansi.StringWidth(label)
	return label, hitRect{x: max(0, m.mainWidth()-width-1), y: y, width: width}, true
}

// status is trusted styled text from workingLine; user-provided fields are
// cleaned there before any foreground styling is added.
func (m *Model) statusLine(t *tab, status string) string {
	w := m.mainWidth()
	label, rect, visible := m.latestButton(t)
	if !visible {
		return fitLine(" "+status, w)
	}
	leftWidth := rect.x
	progress := m.scrollProgress(t)
	if progress != "" && ansi.StringWidth(progress)+1 < leftWidth {
		leftWidth -= ansi.StringWidth(progress) + 1
	} else {
		progress = ""
	}
	left := ""
	if leftWidth > 0 {
		left = fitLine(" "+status, leftWidth)
	}
	if progress != "" {
		left += m.paint(progress, m.cfg.Theme.Muted) + " "
	}
	button := lipgloss.NewStyle().Foreground(lipgloss.Color(m.cfg.Theme.Accent)).Underline(true).Render(label)
	return left + button + strings.Repeat(" ", max(0, w-rect.x-rect.width))
}
