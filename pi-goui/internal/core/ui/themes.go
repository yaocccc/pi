package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func (m *Model) chooseTheme(t *tab, name string) tea.Cmd {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		d := m.newDialog("theme", "Theme", "↑/↓ preview · Enter save · Esc cancel")
		before := m.cfg.Theme
		d.themeBefore = &before
		for _, name := range config.ThemeNames() {
			detail := ""
			if name == "pi" {
				detail = "Native pi palette"
			}
			if name == m.cfg.ThemeName {
				detail += " · current"
				d.selected = len(d.options)
			}
			d.options = append(d.options, option{Label: name, Detail: detail, Value: name})
		}
		t.dialogs = append(t.dialogs, d)
		return m.focus(t)
	}
	theme, err := config.LoadTheme(m.cfg.AgentDir, name)
	if err == nil {
		err = config.SaveTheme(m.cfg.AgentDir, name)
	}
	if err != nil {
		t.notify("Theme: " + err.Error())
		return nil
	}
	m.cfg.ThemeName = name
	m.applyTheme(theme)
	t.notify("Theme: " + name)
	return m.focus(t)
}

func (m *Model) applyTheme(theme config.Theme) {
	if m.cfg.Theme == theme {
		return
	}
	m.cfg.Theme = theme
	for _, tab := range m.tabs {
		tab.dirty = true
		m.resizeEditor(tab)
	}
}

func (m *Model) restoreTheme(d *dialog) {
	if d.themeBefore != nil {
		m.applyTheme(*d.themeBefore)
	}
}

func (m *Model) previewTheme(d *dialog) {
	if d.kind != "theme" || d.themeBefore == nil {
		return
	}
	options := m.filtered(d)
	if len(options) == 0 {
		m.restoreTheme(d)
		return
	}
	d.selected = min(max(0, d.selected), len(options)-1)
	if theme, err := config.LoadTheme(m.cfg.AgentDir, options[d.selected].Value); err == nil {
		m.applyTheme(theme)
	}
}
