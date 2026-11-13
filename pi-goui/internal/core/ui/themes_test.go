package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func TestThemePickerOpenAndSelect(t *testing.T) {
	m := testModel(t, "")
	m.cfg.AgentDir = t.TempDir()
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	tab := m.current()
	if _, handled := m.slash(tab, "/theme"); !handled {
		t.Fatal("theme command not handled")
	}
	if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "theme" {
		t.Fatal("theme picker not opened")
	}
	d := tab.dialogs[0]
	before := m.cfg.Theme
	tab.dirty = false
	m.key(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cfg.Theme == before {
		t.Fatal("navigation did not preview theme")
	}
	if !tab.dirty {
		t.Fatal("theme preview did not invalidate transcript")
	}
	if _, err := os.Stat(filepath.Join(m.cfg.AgentDir, "goui.json")); !os.IsNotExist(err) {
		t.Fatal("preview persisted settings")
	}
	m.View()
	d.editor.SetValue("nord")
	options := m.filtered(d)
	if len(options) != 1 || options[0].Value != "nord" {
		t.Fatalf("unexpected filtered options: %+v", options)
	}
	d.selected = 0
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(tab.dialogs) != 0 {
		t.Fatal("picker remained open")
	}
	want, err := config.LoadTheme(m.cfg.AgentDir, "nord")
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.ThemeName != "nord" || m.cfg.Theme != want {
		t.Fatal("selected theme not applied")
	}
	m.View()
	// Reopening and cancelling must retain the selected palette.
	m.slash(tab, "/theme")
	m.key(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cfg.Theme == want {
		t.Fatal("reopened picker did not preview")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if len(tab.dialogs) != 0 || m.cfg.ThemeName != "nord" || m.cfg.Theme != want {
		t.Fatal("cancel changed theme or left picker open")
	}
}
