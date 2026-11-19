package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func (m *Model) showSettings(t *tab, selected int) tea.Cmd {
	d := m.newDialog("setting", "Frontend settings", "Enter: change and save · Esc: close")
	side := m.cfg.SidebarPosition
	if side == "" {
		side = "right"
	}
	startup := "New session"
	if m.cfg.ResumeLastSession {
		startup = "Resume latest in current directory"
	}
	d.options = []option{
		{Label: "Theme", Detail: m.cfg.ThemeName, Value: "theme"},
		{Label: "Sidebar position", Detail: side, Value: "sidebar"},
		{Label: "On startup", Detail: startup, Value: "startup"},
	}
	d.selected = selected
	t.dialogs = append(t.dialogs, d)
	return m.focus(t)
}

func (m *Model) changeSetting(t *tab, value string) tea.Cmd {
	var err error
	selected := 0
	switch value {
	case "theme":
		return m.chooseTheme(t, "")
	case "sidebar":
		selected = 1
		side := "left"
		if m.cfg.SidebarPosition == "left" {
			side = "right"
		}
		err = config.SaveSidebarPosition(m.cfg.AgentDir, side)
		if err == nil {
			m.cfg.SidebarPosition = side
		}
	case "startup":
		selected = 2
		resume := !m.cfg.ResumeLastSession
		err = config.SaveResumeLastSession(m.cfg.AgentDir, resume)
		if err == nil {
			m.cfg.ResumeLastSession = resume
		}
	}
	if err != nil {
		t.notify("Settings: " + err.Error())
	}
	return m.showSettings(t, selected)
}
