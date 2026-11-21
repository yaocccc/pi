package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func TestLateThemeSaveDoesNotReplaceNewPreview(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	save := m.chooseTheme(tab, "gradient-frost")
	m.chooseTheme(tab, "")
	m.key(tea.KeyPressMsg{Code: tea.KeyDown})
	preview := m.cfg.Theme
	m.Update(commandResult(save))
	if m.cfg.Theme != preview || m.cfg.ThemeName != "gradient-frost" {
		t.Fatal("late save replaced active preview or lost the new default")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	want, err := config.LoadTheme(m.cfg.AgentDir, "gradient-frost")
	if err != nil || m.cfg.Theme != want {
		t.Fatal("cancel restored pre-save theme", err)
	}
}

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
	d.editor.SetValue("gradient-frost")
	options := m.filtered(d)
	if len(options) != 1 || options[0].Value != "gradient-frost" {
		t.Fatalf("unexpected filtered options: %+v", options)
	}
	d.selected = 0
	save := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, err := os.Stat(filepath.Join(m.cfg.AgentDir, "goui.json.lock")); !os.IsNotExist(err) {
		t.Fatalf("selection waited for a file lock: %v", err)
	}
	m.Update(commandResult(save))
	if len(tab.dialogs) != 0 {
		t.Fatal("picker remained open")
	}
	want, err := config.LoadTheme(m.cfg.AgentDir, "gradient-frost")
	if err != nil {
		t.Fatal(err)
	}
	if m.cfg.ThemeName != "gradient-frost" || m.cfg.Theme != want {
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
	if len(tab.dialogs) != 0 || m.cfg.ThemeName != "gradient-frost" || m.cfg.Theme != want {
		t.Fatal("cancel changed theme or left picker open")
	}
}
