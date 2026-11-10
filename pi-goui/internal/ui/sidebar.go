package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
)

func (m *Model) sidebarContext() render.SidebarContext {
	directory := m.tabDirectory(m.current())
	context := render.SidebarContext{Directory: directory}
	if m.gitCwd == directory {
		context.Git = m.gitInfo
	}
	key, _ := m.quotaIdentity()
	if key != "" && key == m.quotaKey && m.quotaSnapshot.LoggedIn {
		s := m.quotaSnapshot
		context.Quota = &render.SidebarQuota{RemainingPercent: s.RemainingPercent, ResetAt: s.ResetAt, Credits: s.Credits, UnlimitedCredits: s.UnlimitedCredits, Now: time.Now()}
	}
	return context
}
func (m *Model) sidebarLayout() render.SidebarLayout {
	return render.SidebarGeometry(m.height, m.sidebarContext())
}
func (m *Model) revealSidebar() {
	if len(m.tabs) == 0 {
		m.sidebarOffset = 0
		return
	}
	visible := max(1, m.sidebarLayout().Capacity)
	m.sidebarOffset = min(m.sidebarOffset, max(0, len(m.tabs)-visible))
	if m.active < m.sidebarOffset {
		m.sidebarOffset = m.active
	}
	if m.active >= m.sidebarOffset+visible {
		m.sidebarOffset = m.active - visible + 1
	}
}
func (m *Model) requestGit(now time.Time) tea.Cmd {
	directory := m.sidebarContext().Directory
	if m.gitPending || (directory == m.gitCwd && now.Sub(m.gitChecked) < 5*time.Second) {
		return nil
	}
	m.gitPending = true
	m.gitChecked = now
	return m.git.command(directory)
}
