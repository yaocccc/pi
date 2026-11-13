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
	if t == nil || len(t.dialogs) > 0 || m.inspectorFullscreen() || m.sidebarWidth() == 0 || y < 0 || y >= m.height {
		return false
	}
	return x == m.mainWidth()
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
	m.panelResize.detail = m.inspectorOpen()
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
	minimum := 24
	if m.panelResize.detail {
		minimum = 20
	}
	width := min(max(minimum, m.width-v.X), max(minimum, m.width-40))
	if width == m.sidebarWidth() {
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
		m.inspectorOpen() == m.panelResize.detail && m.sidebarWidth() > 0 && !m.inspectorFullscreen()
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
		t.dirty = true
	}
	if t := m.current(); t != nil {
		m.resizeEditor(t)
	}
}
