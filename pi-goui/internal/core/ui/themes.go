package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func (m *Model) chooseTheme(t *tab, name string) tea.Cmd {
	name = config.CanonicalThemeName(strings.ToLower(strings.TrimSpace(name)))
	if name == "" {
		d := m.newDialog("theme", "Theme", "↑/↓ preview · Enter save · Esc cancel")
		before := m.cfg.Theme
		d.themeBefore = &before
		for _, name := range append(config.ThemeNames(), "random") {
			detail := ""
			if name == config.DefaultThemeName {
				detail = "Native pi palette"
			} else if name == "random" {
				detail = "Pick a theme at each startup"
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
	if err != nil {
		t.notify("Theme: " + err.Error())
		return nil
	}
	q := &m.preferenceSaves.queued
	q.themeName, q.palette, q.tabID = name, &theme, t.id
	return tea.Batch(m.savePendingPreferences(), m.focus(t))
}

func (m *Model) finishTheme(name string, theme config.Theme, tabID int) {
	m.cfg.ThemeName = name
	// A picker may have opened after Enter. Keep its live preview, but
	// make Escape return to the newly committed palette.
	previewing := false
	for _, tab := range m.tabs {
		for _, d := range tab.dialogs {
			if d.themeBefore != nil {
				d.themeBefore = &theme
				previewing = previewing || tab == m.current()
			}
		}
	}
	if !previewing {
		m.applyTheme(theme)
	}
	if t := m.byID(tabID); t != nil {
		t.notify("Theme: " + name)
	}
}

func (m *Model) applyTheme(theme config.Theme) {
	if m.cfg.Theme == theme {
		return
	}
	m.cfg.Theme = theme
	for _, tab := range m.tabs {
		tab.invalidateDocument()
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
