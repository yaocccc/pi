package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const forkButton = "[从此轮分叉]"

// Only main transcript user headers are interactive. The overlay never enters
// Document.Text/Content, so copying a message cannot copy action labels.
func (m *Model) forkTarget(x, y int) (string, hitRect, bool) {
	t := m.current()
	w := ansi.StringWidth(forkButton)
	if t == nil || len(t.dialogs) > 0 || t.forkPending || t.selection.dragging || m.inspector.selection.dragging || m.panelResize.dragging || m.height < 8 || m.mainWidth() < w+8 || m.inspectorFullscreen() || x < 0 || x >= m.mainWidth() || y < m.chatTop(m.current()) || y >= m.chatTop(m.current())+m.transcriptHeight(t) {
		return "", hitRect{}, false
	}
	if box, ok := m.toastBox(t, time.Now()); ok && box.contains(x, y) {
		return "", hitRect{}, false
	}
	if t.footerHover.active != "" {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if box.contains(x, y) {
			return "", hitRect{}, false
		}
	}
	if m.codexFastPopupVisible() {
		target, box, _ := m.codexFastGeometry()
		if target.contains(x, y) || box.contains(x, y) {
			return "", hitRect{}, false
		}
	}
	row := t.offset + y - m.chatTop(m.current())
	lines := strings.Split(t.doc.Text, "\n")
	for _, b := range t.doc.Blocks {
		if b.Role != "user" || row < b.Start || row >= b.End || b.Start < t.offset || b.Start >= t.offset+m.transcriptHeight(t) || b.Start >= len(lines) {
			continue
		}
		// On short bubbles use the empty left gutter; otherwise replace only
		// the decorative top border to the right of the user title.
		header := ansi.Strip(lines[b.Start])
		left := ansi.StringWidth(header) - ansi.StringWidth(strings.TrimLeft(header, " "))
		buttonX := m.mainWidth() - w - 1
		if left >= w+1 {
			buttonX = left - w - 1
		}
		return strings.TrimSuffix(b.ID, "/0"), hitRect{x: buttonX, y: b.Start - t.offset + m.chatTop(m.current()), width: w}, true
	}
	return "", hitRect{}, false
}

func (m *Model) overlayFork(rows []string) {
	t := m.current()
	if t == nil {
		return
	}
	if !m.sidebarHistory.pointerKnown {
		return
	}
	_, rect, ok := m.forkTarget(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)
	if !ok {
		return
	}
	i := rect.y - m.chatTop(m.current())
	if i < 0 || i >= len(rows) {
		return
	}
	row := rows[i]
	rows[i] = popupBackdropSlice(row, 0, rect.x) + m.paint(forkButton, m.inspectorButtonColor(rect)) + popupBackdropSlice(row, rect.x+rect.width, ansi.StringWidth(row))
}
