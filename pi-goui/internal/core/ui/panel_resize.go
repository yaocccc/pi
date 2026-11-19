package ui

import tea "charm.land/bubbletea/v2"

// Preferences belong to the running Model, never to persisted configuration.
type panelResizeState struct {
	sidebar, inspector int
	dragging           bool
	detail             bool
	tab                *tab
}

func (m *Model) panelResizeTarget(x, y int) bool {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || m.inspectorFullscreen() || y < 0 || y >= m.height {
		return false
	}
	if m.detailWidth() > 0 && x == m.mainWidth() {
		return true
	}
	sideEdge := m.sideX()
	if m.cfg.SidebarPosition == "left" {
		sideEdge = -1
	}
	return m.sidebarWidth() > 0 && x == sideEdge
}

func (m *Model) beginPanelResize(v tea.Mouse) bool {
	if v.Mod&tea.ModShift != 0 || !m.panelResizeTarget(v.X, v.Y) {
		return false
	}
	// Warm the immutable rows once. During motion only the visible viewport is
	// clipped/padded; lexing and wrapping the full result wait for release.
	if m.inspectorOpen() {
		m.inspectorRows()
	}
	m.panelResize.dragging = true
	m.panelResize.detail = m.detailWidth() > 0 && v.X == m.mainWidth()
	m.panelResize.tab = m.current()
	m.current().selection = transcriptSelection{}
	m.inspector.selection = transcriptSelection{}
	m.extensionControls.Hover = false
	return true
}

func (m *Model) movePanelResize(v tea.Mouse) bool {
	if !m.panelResize.dragging {
		return false
	}
	if !m.panelResizeValid() || v.Button != tea.MouseLeft {
		m.finishPanelResize()
		return true
	}
	main, detail, sidebar := m.paneWidths()
	minimum, current, width := 24, sidebar, m.width-v.X
	if m.panelResize.detail {
		minimum, current, width = 20, detail, main+detail-v.X
	} else if m.cfg.SidebarPosition == "left" {
		width = m.mainScreenX() + v.X + 1
	}
	width = min(max(minimum, width), main+current-40)
	if width == current {
		return true
	}
	if m.panelResize.detail {
		m.panelResize.inspector = width
	} else {
		m.panelResize.sidebar = width
	}
	return true
}

func (m *Model) panelResizeValid() bool {
	t := m.current()
	return t != nil && t == m.panelResize.tab && len(t.dialogs) == 0 &&
		((m.panelResize.detail && m.detailWidth() > 0) || (!m.panelResize.detail && m.sidebarWidth() > 0))
}

func (m *Model) finishPanelResize() {
	if !m.panelResize.dragging {
		return
	}
	m.panelResize.dragging = false
	m.panelResize.tab = nil
	// Update only the current editor now. Activation already resizes other
	// editors; dirty flags retain any streamed updates while display was frozen.
	for _, t := range m.tabs {
		t.invalidateDocument()
	}
	if t := m.current(); t != nil {
		m.resizeEditor(t)
	}
}
