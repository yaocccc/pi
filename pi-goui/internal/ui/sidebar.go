package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func (m *Model) sidebarContext() render.SidebarContext {
	directory := m.tabDirectory(m.current())
	context := render.SidebarContext{Directory: directory, Mode: m.codexFastMode(), Quota: &render.SidebarQuota{Now: time.Now()}, TodoFrame: m.frame}
	if t := m.current(); t != nil {
		if !t.todoExpiry.hidden || t.todoExpiry.revision != t.state.PlanRevision || t.todoExpiry.file != t.state.File {
			context.Todo = t.state.Plan
		}
		if t.todoRevision == t.state.PlanRevision {
			context.TodoScroll = t.todoScroll
		}
	}
	if m.sidebarHistory.cwd == directory {
		context.HistoryOffset = m.sidebarHistory.offset
		list := m.sidebarSummaries()
		for _, row := range session.ThreadRows(list) {
			saved := list[row.Index]
			context.HistoryDepth = append(context.HistoryDepth, row.Depth)
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

// Render, click and tooltip all resolve the same threaded row order.
func (m *Model) sidebarSummaries() []session.Summary {
	list := append([]session.Summary(nil), m.sidebarHistory.sessions...)
	for i, saved := range list {
		for _, t := range m.tabs {
			if sameSessionPath(t.state.File, saved.Path) && t.state.ID == saved.ID {
				list[i].Name = t.state.Name
				if t.origin.ID == t.state.ID {
					list[i].ParentSession, list[i].Fork = t.origin.ParentSession, t.origin.Fork
				}
				break
			}
		}
	}
	return list
}
func (m *Model) sidebarHistoryAt(index int) session.Summary {
	list := m.sidebarSummaries()
	rows := session.ThreadRows(list)
	if index < 0 || index >= len(rows) {
		return session.Summary{}
	}
	return list[rows[index].Index]
}

// Hover uses the same inner card rectangle and scroll offset as rendering.
func (m *Model) sidebarSessionHovered(index int) bool {
	t := m.current()
	h := m.sidebarHistory
	if t == nil || len(t.dialogs) > 0 || m.inspectorOpen() || m.codexFastPopupVisible() || !h.pointerKnown || m.sidebarWidth() == 0 || h.pointerX < m.mainWidth()+2 || h.pointerX >= m.width-2 {
		return false
	}
	layout := m.sidebarLayout()
	row := h.pointerY - layout.SessionsStart
	return row >= 0 && row < layout.Capacity*render.SidebarSessionRows && row/render.SidebarSessionRows+m.sidebarOffset == index
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
	generation         uint64
	checked            time.Time
}

type sidebarHistoryMsg struct {
	generation uint64
	cwd        string
	sessions   []session.Summary
	err        error
}

func (m *Model) requestSidebarHistory(now time.Time) tea.Cmd {
	cwd := m.tabDirectory(m.current())
	h := &m.sidebarHistory
	if m.opts.NoSession || h.pending || (cwd == h.cwd && now.Sub(h.checked) < 5*time.Second) {
		return nil
	}
	h.pending = true
	h.checked = now
	h.generation++
	generation := h.generation
	root := m.cfg.AgentDir
	return func() tea.Msg {
		list, err := session.List(root, cwd)
		return sidebarHistoryMsg{generation: generation, cwd: cwd, sessions: list, err: err}
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
