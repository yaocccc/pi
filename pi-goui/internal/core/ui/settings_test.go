package ui

import (
	"errors"
	"os"
	"path/filepath"
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
	key := func(k rune) tea.Cmd { return m.key(tea.KeyPressMsg{Code: k}) }
	key(tea.KeyDown)
	first := key(tea.KeyEnter)
	key(tea.KeyDown)
	key(tea.KeyEnter)
	if _, err := os.Stat(filepath.Join(dir, "goui.json.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("key handler touched preferences before command execution: %v", err)
	}
	if m.cfg.SidebarPosition != "right" || m.cfg.ResumeLastSession {
		t.Fatal("applied settings before persistence")
	}
	if side, resume := m.desiredPreferences(); side != "left" || !resume {
		t.Fatalf("queued changes not visible to the settings picker: %s %v", side, resume)
	}

	saved := commandResult(first).(preferenceSavedMsg)
	if saved.err != nil {
		t.Fatal(saved.err)
	}
	next := m.finishPreferenceSave(saved)
	if m.cfg.SidebarPosition != "left" || m.cfg.ResumeLastSession {
		t.Fatal("result did not apply only the saved fields")
	}
	if cmd := m.finishPreferenceSave(saved); cmd != nil || m.cfg.ResumeLastSession {
		t.Fatal("late result applied while a newer write is active")
	}
	second := next().(preferenceSavedMsg)
	if second.err != nil {
		t.Fatal(second.err)
	}
	m.Update(second)
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

func TestPreferenceSaveCoalescesWhileActive(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	toggle := func(setting string) tea.Cmd {
		tab.dialogs = nil
		return m.changeSetting(tab, setting)
	}
	first := toggle("sidebar") // left
	active := m.preferenceSaves.active
	toggle("sidebar") // right
	toggle("sidebar") // left
	toggle("sidebar") // right
	toggle("startup") // resume
	if m.preferenceSaves.active != active {
		t.Fatal("queued toggles replaced the in-flight transaction")
	}
	next := m.finishPreferenceSave(commandResult(first).(preferenceSavedMsg))
	if change := m.preferenceSaves.active; change == nil || change.sidebar == nil || *change.sidebar != "right" || change.resume == nil || !*change.resume {
		t.Fatalf("pending toggles did not merge: %+v", change)
	}
	saved := next().(preferenceSavedMsg)
	if saved.err != nil {
		t.Fatal(saved.err)
	}
	if cmd := m.finishPreferenceSave(saved); cmd != nil || m.preferenceSaves.active != nil {
		t.Fatal("more than two transactions for a burst of edits")
	}
	cfg, err := config.Load(t.TempDir())
	if err != nil || cfg.SidebarPosition != "right" || !cfg.ResumeLastSession {
		t.Fatalf("unexpected final preference values: %v", err)
	}
}

func TestExitFlushesPreferencesBeforeLateCommand(t *testing.T) {
	m := testModel(t, "")
	first := m.changeSetting(m.current(), "sidebar")
	m.changeSetting(m.current(), "sidebar") // latest position is right
	m.changeSetting(m.current(), "startup")
	if err := m.preferenceSaves.close(m.cfg.AgentDir); err != nil {
		t.Fatal(err)
	}
	m.finishPreferenceSave(commandResult(first).(preferenceSavedMsg))
	cfg, err := config.Load(t.TempDir())
	if err != nil || cfg.SidebarPosition != "right" || !cfg.ResumeLastSession {
		t.Fatalf("exit or late command lost queued preferences: %v", err)
	}
}

func TestPreferenceSaveFailureDoesNotApplyOrReopenSettings(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.cfg.AgentDir = filepath.Join(m.cfg.AgentDir, "not-a-directory")
	if err := os.WriteFile(m.cfg.AgentDir, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := m.changeSetting(tab, "sidebar")
	tab.dialogs = nil // User has since dismissed the settings picker.
	v := commandResult(cmd).(preferenceSavedMsg)
	if v.err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	m.finishPreferenceSave(v)
	if m.cfg.SidebarPosition != "right" || len(tab.dialogs) != 0 || tab.toast.text == "" {
		t.Fatal("failure applied the change, reopened settings, or was silent")
	}
}
