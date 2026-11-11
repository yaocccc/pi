package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"strings"
	"time"
)

// Pointer shapes use OSC 22; terminals without support keep their usual cursor.
func (m *Model) updatePointer() tea.Cmd {
	if !m.sidebarHistory.pointerKnown {
		return nil
	}
	shape := m.pointerShapeAt(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)
	if shape == m.pointerShape {
		return nil
	}
	m.pointerShape = shape
	return tea.Raw(ansi.SetPointerShape(shape))
}

func (m *Model) pointerShapeAt(x, y int) string {
	t := m.current()
	if t == nil || x < 0 || x >= m.width || y < 0 || y >= m.height {
		return "text"
	}
	if m.codexFastPopupVisible() {
		target, box, _ := m.codexFastGeometry()
		if target.contains(x, y) {
			return "pointer"
		}
		if box.contains(x, y) {
			return "pointer"
		}
	}
	if t.footerHover.active != "" {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if box.contains(x, y) {
			return "pointer"
		}
	}
	if box, ok := m.sidebarHistoryTooltip(t); ok && x >= box.x && x < box.x+box.width && y >= box.y && y < box.y+box.height {
		return "pointer"
	}
	if m.sidebarWidth() > 0 && x >= m.mainWidth() {
		layout := m.sidebarLayout()
		if y == layout.DirectoryRow && x >= m.mainWidth()+2 && x < m.width-2 {
			return "pointer"
		}
		if y >= layout.HistoryStart && y < layout.HistoryStart+layout.HistoryCount*render.SidebarHistoryRows && y < m.height-1 && x >= m.mainWidth()+2 && x < m.width-2 {
			return "pointer"
		}
		newStart, newWidth := render.SidebarNewButton(max(0, m.sidebarWidth()-4))
		newX := m.mainWidth() + 2 + newStart
		if y == layout.NewRow && y < m.height-1 && x >= newX && x < newX+newWidth {
			return "pointer"
		}
		row := y - layout.SessionsStart
		if row >= 0 && row < layout.Capacity*render.SidebarSessionRows && row/render.SidebarSessionRows+m.sidebarOffset < len(m.tabs) {
			return "pointer"
		}
		return "text"
	}
	if len(t.dialogs) > 0 {
		p := m.popupGeometry(t.dialogs[0])
		if x >= p.x && x < p.x+p.width && y >= p.y && y < p.y+p.height {
			return "pointer"
		}
		return "text"
	}
	if box, ok := m.toastBox(t, time.Now()); ok && box.contains(x, y) {
		return "pointer"
	}
	if m.searching != "" {
		return "text"
	}
	if m.footerTargetKind(t) != "" {
		return "pointer"
	}
	if _, rect, visible := m.latestButton(t); visible && rect.contains(x, y) {
		return "pointer"
	}
	if m.height >= 8 && y >= transcriptTop && y < transcriptTop+m.transcriptHeight(t) {
		row := y - transcriptTop + t.offset
		if row >= strings.Count(t.doc.Text, "\n")+1 {
			return "text"
		}
		for _, link := range t.doc.ImageLinks {
			if row == link.Row && x >= link.Start && x < link.End {
				return "pointer"
			}
		}
		for _, b := range t.doc.Blocks {
			if b.Foldable && row >= b.Start && row < b.End {
				return "pointer"
			}
		}
		return "text"
	}
	return "text"
}
