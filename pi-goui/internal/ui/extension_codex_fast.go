package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
	"time"
)

var codexFastLevels = hostui.CodexFastLevels

type codexFastMsg = hostui.CodexResult

func (m *Model) openCodexFastSettings(d *dialog, payload string) bool {
	h, v, done := m.extensionDialog(nil, d)
	defer done()
	return h.OpenCodexFastSettings(v, payload)
}
func (m *Model) codexFastMode() string {
	if t := m.current(); t != nil {
		return t.codex.Mode(t.dead)
	}
	return ""
}
func (m *Model) requestCodexFast(t *tab, level string, now time.Time) tea.Cmd {
	if t == nil {
		return nil
	}
	return t.codex.Request(t.id, t.client, t.dead, level, now)
}
func (m *Model) codexFastResult(v codexFastMsg) {
	if t := m.byID(v.ID); t != nil {
		t.codex.Apply(v, t.dead, t.notify)
	}
}
func (m *Model) codexView() hostui.CodexView {
	v := hostui.CodexView{Blocked: true}
	t := m.current()
	if t == nil {
		return v
	}
	return hostui.CodexView{State: t.codex.State, Blocked: t.dead || m.inspectorOpen() || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "", PointerKnown: m.sidebarHistory.pointerKnown, Hover: m.codexFastHover, SidebarWidth: m.sidebarWidth(), MainWidth: m.mainWidth(), Width: m.width, Height: m.height, ModeRow: m.sidebarLayout().ModeRow, PointerX: m.sidebarHistory.pointerX, PointerY: m.sidebarHistory.pointerY, Theme: m.cfg.Theme}
}
func (m *Model) codexFastGeometry() (hitRect, footerPopupRect, bool) {
	t, b, ok := m.codexView().Geometry()
	return hitRect{x: t.X, y: t.Y, width: t.Width}, footerPopupRect{x: b.X, y: b.Y, width: b.Width, height: b.Height}, ok
}
func (m *Model) codexFastPopupVisible() bool { return m.codexView().Visible() }
func (m *Model) clickCodexFast(x, y int) bool {
	handled, choice := m.codexView().Click(x, y)
	m.codexFastChoice = choice
	if choice != "" {
		m.codexFastHover = false
	}
	return handled
}
func (m *Model) codexFastClickCommand(x, y int) (tea.Cmd, bool) {
	m.codexFastChoice = ""
	handled := m.clickCodexFast(x, y)
	choice := m.codexFastChoice
	m.codexFastChoice = ""
	if !handled || choice == "" {
		return nil, handled
	}
	return m.requestCodexFast(m.current(), choice, time.Now()), true
}
func (m *Model) overlayCodexFast(view string) string {
	return m.codexView().Overlay(view, popupBackdropSlice)
}
