package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func (m *Model) sidebarContext() render.SidebarContext {
	directory := m.tabDirectory(m.current())
	context := render.SidebarContext{Directory: directory, Quota: &render.SidebarQuota{Now: time.Now()}}
	if m.sidebarHistory.cwd == directory {
		context.HistoryOffset = m.sidebarHistory.offset
		for _, saved := range m.sidebarHistory.sessions {
			name := saved.Name
			if name == "" {
				name = saved.FirstMessage
			}
			if name == "" {
				name = saved.ID
			}
			context.History = append(context.History, name)
		}
	}
	if m.gitCwd == directory {
		context.Git = m.gitInfo
	}
	key, _ := m.quotaIdentity()
	if t := m.current(); t != nil && t.state.Model.Provider == "" {
		key = m.quotaKey // Keep cached quota while a new session initializes.
	}
	if key != "" && key == m.quotaKey && m.quotaSnapshot.LoggedIn {
		s := m.quotaSnapshot
		context.Quota = &render.SidebarQuota{RemainingPercent: s.RemainingPercent, ResetAt: s.ResetAt, Credits: s.Credits, UnlimitedCredits: s.UnlimitedCredits, Now: time.Now()}
	}
	h := m.sidebarHistory
	if h.pointerKnown && m.sidebarWidth() > 0 && h.pointerX >= m.mainWidth()+2 && h.pointerX < m.width-2 && h.pointerY < m.height-1 {
		layout := render.SidebarGeometry(m.height, context)
		row := h.pointerY - layout.HistoryStart
		if row >= 0 && row < layout.HistoryCount*render.SidebarHistoryRows {
			context.HistoryHovered = make([]bool, len(context.History))
			context.HistoryHovered[layout.HistoryOffset+row/render.SidebarHistoryRows] = true
		}
	}
	return context
}
func (m *Model) sidebarLayout() render.SidebarLayout {
	return render.SidebarGeometry(m.height, m.sidebarContext())
}
func (m *Model) revealSidebar() {
	m.sidebarHistory.offset = m.sidebarLayout().HistoryOffset
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

type sidebarHistoryState struct {
	pointerKnown       bool
	pointerX, pointerY int
	offset             int
	cwd                string
	sessions           []session.Summary
	pending            bool
	checked            time.Time
}

type sidebarHistoryMsg struct {
	cwd      string
	sessions []session.Summary
	err      error
}

func (m *Model) requestSidebarHistory(now time.Time) tea.Cmd {
	cwd := m.tabDirectory(m.current())
	h := &m.sidebarHistory
	if m.opts.NoSession || h.pending || (cwd == h.cwd && now.Sub(h.checked) < 5*time.Second) {
		return nil
	}
	h.pending = true
	h.checked = now
	root := m.cfg.AgentDir
	return func() tea.Msg {
		list, err := session.List(root, cwd)
		return sidebarHistoryMsg{cwd: cwd, sessions: list, err: err}
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
