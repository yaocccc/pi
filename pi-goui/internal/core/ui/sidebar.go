package ui

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func (m *Model) sidebarContext() render.SidebarContext {
	directory := m.tabDirectory(m.current())
	context := render.SidebarContext{Directory: directory, Quota: &render.SidebarQuota{Now: time.Now()}, TodoFrame: m.frame}
	m.extendSidebar(&context)
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
	type identity struct{ path, id string }
	live := make(map[identity]*tab, len(m.tabs))
	for _, t := range m.tabs {
		if t.state.File != "" {
			key := identity{filepath.Clean(t.state.File), t.state.ID}
			if live[key] == nil {
				live[key] = t // Preserve the first matching tab's precedence.
			}
		}
	}
	for i, saved := range list {
		if saved.Path == "" {
			continue
		}
		if t := live[identity{filepath.Clean(saved.Path), saved.ID}]; t != nil {
			list[i].Name = t.state.Name
			if t.origin.ID == t.state.ID {
				list[i].ParentSession, list[i].Fork = t.origin.ParentSession, t.origin.Fork
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
func (m *Model) sidebarSessionHoverIndex(layout render.SidebarLayout) int {
	t := m.current()
	h := m.sidebarHistory
	if t == nil || len(t.dialogs) > 0 || m.inspectorOpen() || !h.pointerKnown || m.sidebarWidth() == 0 || h.pointerX < m.mainWidth()+2 || h.pointerX >= m.width-2 || m.extensionPopupVisible() {
		return -1
	}
	row := h.pointerY - layout.SessionsStart
	if row >= 0 && row < layout.Capacity*render.SidebarSessionRows {
		return row/render.SidebarSessionRows + m.sidebarOffset
	}
	return -1
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

type sidebarHistoryCache struct {
	sessions []session.Summary
	offset   int
	checked  time.Time
}

type sidebarHistoryState struct {
	cache              map[string]sidebarHistoryCache
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

// Restore a directory immediately; disk refreshes never clear its visible list.
func (m *Model) restoreSidebarHistory() {
	cwd := m.tabDirectory(m.current())
	h := &m.sidebarHistory
	if h.cwd == cwd || m.opts.NoSession {
		return
	}
	if h.cache == nil {
		h.cache = make(map[string]sidebarHistoryCache)
	}
	if h.cwd != "" {
		checked := h.checked
		if h.pending {
			checked = time.Time{}
		}
		h.cache[h.cwd] = sidebarHistoryCache{h.sessions, h.offset, checked}
	}
	cached := h.cache[cwd]
	h.cwd, h.sessions, h.offset, h.checked = cwd, cached.sessions, cached.offset, cached.checked
	h.pending = false
	h.generation++ // Ignore a scan for the directory we just left.
}

func (m *Model) requestSidebarHistory(now time.Time) tea.Cmd {
	m.restoreSidebarHistory()
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
	directory := m.tabDirectory(m.current())
	if m.gitPending || (directory == m.gitCwd && now.Sub(m.gitChecked) < 5*time.Second) {
		return nil
	}
	m.gitPending = true
	m.gitChecked = now
	return m.git.command(directory)
}
