package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
)

func TestSettingChangesPersist(t *testing.T) {
	m := testModel(t, "")
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m.cfg.AgentDir = dir
	tab := m.current()
	if _, handled := m.slash(tab, "/setting"); !handled || len(tab.dialogs) != 1 || len(tab.dialogs[0].options) != 3 {
		t.Fatal("settings picker not opened")
	}
	key := func(k rune) { m.key(tea.KeyPressMsg{Code: k}) }
	key(tea.KeyDown)
	key(tea.KeyEnter)
	key(tea.KeyDown)
	key(tea.KeyEnter)
	cfg, err := config.Load(t.TempDir())
	if err != nil || cfg.SidebarPosition != "left" || !cfg.ResumeLastSession || m.cfg.SidebarPosition != "left" || !m.cfg.ResumeLastSession {
		t.Fatalf("settings not applied and saved: %v", err)
	}
	key(tea.KeyEscape)
	m.slash(tab, "/setting")
	key(tea.KeyEnter)
	if len(tab.dialogs) != 1 || tab.dialogs[0].kind != "theme" {
		t.Fatal("settings did not open theme preview")
	}
}
