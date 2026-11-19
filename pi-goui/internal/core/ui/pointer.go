package ui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
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
	if m.panelResize.dragging || m.panelResizeTarget(x, y) {
		return "ew-resize"
	}
	t := m.current()
	if t == nil || x < -m.mainScreenX() || x >= m.width-m.mainScreenX() || y < 0 || y >= m.height {
		return "text"
	}
	if m.extensionPopupVisible() {
		target, box, _ := m.extensionPopupGeometry()
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
	if m.inspectorOpen() && len(t.dialogs) > 0 {
		p := m.popupGeometry(t.dialogs[0])
		if x >= p.x && x < p.x+p.width && y >= p.y && y < p.y+p.height {
			return "pointer"
		}
		return "text"
	}
	if m.inspectorContains(x, y) {
		if rect, ok := m.inspectorCloseTarget(x, y); ok && rect.contains(x, y) {
			return "pointer"
		}
		left, _ := m.inspectorBounds()
		if m.inspector.parent != nil && y == 1 && x >= left+m.inspectorInset() && x < left+m.inspectorInset()+ansi.StringWidth("← 返回") {
			return "pointer"
		}
		if _, ok := m.extensionInputTarget(x, y); ok {
			return "pointer"
		}
		if _, rect, ok := m.conversationInspectTarget(x, y); ok && rect.contains(x, y) {
			return "pointer"
		}
		if view := m.inspector.view(); view.Conversation && y >= 2 && y < m.height-2 {
			row := m.inspectorPoint(x, y).line
			for _, b := range view.Document.Blocks {
				if b.Foldable && row >= b.Start && row < b.End {
					return "pointer"
				}
			}
		}
		return "text"
	}
	if m.sideContainsX(x, 0) {
		layout := m.sidebarLayout()
		if y == layout.DirectoryRow && m.sideContainsX(x, 2) {
			return "pointer"
		}
		if y >= layout.HistoryStart && y < layout.HistoryStart+layout.HistoryCount*render.SidebarHistoryRows && y < m.height-1 && m.sideContainsX(x, 2) {
			return "pointer"
		}
		newStart, newWidth := render.SidebarNewButton(max(0, m.sidebarWidth()-4))
		newX := m.sideX() + 2 + newStart
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
	if m.footerTargetKind(t, x, y) != "" {
		return "pointer"
	}
	if _, rect, visible := m.latestButton(t); visible && rect.contains(x, y) {
		return "pointer"
	}
	if _, rect, ok := m.forkTarget(x, y); ok && rect.contains(x, y) {
		return "pointer"
	}
	if m.height >= 8 && y >= transcriptTop && y < transcriptTop+m.transcriptHeight(t) {
		row := y - transcriptTop + t.offset
		if row >= len(t.documentLines()) {
			return "text"
		}
		for _, link := range t.doc.ImageLinks {
			if row == link.Row && x >= link.Start && x < link.End {
				return "pointer"
			}
		}
		if target, ok := t.doc.MessageTargetAt(row, x); ok {
			if _, valid := resolveMessageTarget(m.tabDirectory(t), target); valid {
				return "pointer"
			}
		}
		if i := blockAtOrBefore(t.doc.Blocks, row); i >= 0 {
			b := t.doc.Blocks[i]
			if b.Foldable && row < b.End {
				return "pointer"
			}
			// Extension children can have no inspector action but still
			// belong to a foldable outer tool. Preserve that hit area.
			for j := i - 1; j >= 0 && t.doc.Blocks[j].Role != "user"; j-- {
				if parent := t.doc.Blocks[j]; parent.Foldable && row < parent.End {
					return "pointer"
				}
				if t.doc.Blocks[j].Role == "tool" || t.doc.Blocks[j].Role == "toolResult" {
					break
				}
			}
		}
		return "text"
	}
	return "text"
}
