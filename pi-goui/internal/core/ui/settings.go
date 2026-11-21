package ui

import (
	"errors"
	"sync"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

type preferenceChange struct {
	themeName string
	palette   *config.Theme
	sidebar   *string
	resume    *bool
	tabID     int
}

// Only the UI goroutine touches the queue. The command reads a frozen change
// and the agent path, never the model. There is at most one outstanding write.
type preferenceSaveState struct {
	active *preferenceChange
	queued preferenceChange
	write  func() error // shared by the in-flight command and graceful shutdown
}

type preferenceSavedMsg struct {
	change *preferenceChange
	err    error
}

func (m *Model) desiredPreferences() (string, bool) {
	side, resume := m.cfg.SidebarPosition, m.cfg.ResumeLastSession
	for _, change := range []*preferenceChange{m.preferenceSaves.active, &m.preferenceSaves.queued} {
		if change == nil {
			continue
		}
		if change.sidebar != nil {
			side = *change.sidebar
		}
		if change.resume != nil {
			resume = *change.resume
		}
	}
	if side == "" {
		side = "right"
	}
	return side, resume
}

func (m *Model) settingsOptions() []option {
	side, resume := m.desiredPreferences()
	startup := "New session"
	if resume {
		startup = "Resume latest in current directory"
	}
	return []option{
		{Label: "Theme", Detail: m.cfg.ThemeName, Value: "theme"},
		{Label: "Sidebar position", Detail: side, Value: "sidebar"},
		{Label: "On startup", Detail: startup, Value: "startup"},
	}
}

func (m *Model) showSettings(t *tab, selected int) tea.Cmd {
	d := m.newDialog("setting", "Frontend settings", "Enter: change and save · Esc: close")
	d.options = m.settingsOptions()
	d.selected = selected
	t.dialogs = append(t.dialogs, d)
	return m.focus(t)
}

func (m *Model) changeSetting(t *tab, value string) tea.Cmd {
	if value == "theme" {
		return m.chooseTheme(t, "")
	}
	side, resume := m.desiredPreferences()
	q := &m.preferenceSaves.queued
	selected := 0
	switch value {
	case "sidebar":
		selected = 1
		if side == "left" {
			side = "right"
		} else {
			side = "left"
		}
		q.sidebar = &side
	case "startup":
		selected = 2
		resume = !resume
		q.resume = &resume
	}
	q.tabID = t.id
	return tea.Batch(m.savePendingPreferences(), m.showSettings(t, selected))
}

func (m *Model) savePendingPreferences() tea.Cmd {
	s := &m.preferenceSaves
	if s.active != nil || (s.queued.sidebar == nil && s.queued.resume == nil && s.queued.themeName == "") {
		return nil
	}
	change, dir := s.queued, m.cfg.AgentDir
	s.active, s.queued = &change, preferenceChange{}
	write := sync.OnceValue(func() error { return change.save(dir) })
	s.write = write
	return func() tea.Msg { return preferenceSavedMsg{&change, write()} }
}

func (c preferenceChange) save(dir string) error {
	return config.SaveFrontendPreferences(dir, c.sidebar, c.resume, c.themeName)
}

// After the event loop exits there may be no result message to dispatch
// the final queued write. Join (or run) the active command before flushing it.
func (s *preferenceSaveState) close(dir string) error {
	if s.write == nil {
		return nil
	}
	err := errors.Join(s.write(), s.queued.save(dir))
	*s = preferenceSaveState{}
	return err
}

func (m *Model) finishPreferenceSave(v preferenceSavedMsg) tea.Cmd {
	if v.change == nil || v.change != m.preferenceSaves.active {
		return nil
	}
	m.preferenceSaves.active, m.preferenceSaves.write = nil, nil
	if v.err == nil {
		if v.change.palette != nil {
			m.finishTheme(v.change.themeName, *v.change.palette, v.change.tabID)
		}
		if v.change.sidebar != nil {
			m.cfg.SidebarPosition = *v.change.sidebar
		}
		if v.change.resume != nil {
			m.cfg.ResumeLastSession = *v.change.resume
		}
	} else if t := m.byID(v.change.tabID); t != nil {
		t.notify("Settings: " + v.err.Error())
	}
	for _, t := range m.tabs {
		for _, d := range t.dialogs {
			if d.kind == "setting" {
				d.options = m.settingsOptions()
			}
		}
	}
	return m.savePendingPreferences()
}
