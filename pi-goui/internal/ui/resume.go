package ui

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) resumeTarget(file string) (string, bool) {
	t := m.current()
	if t == nil {
		return "", false
	}
	if m.opts.NoSession {
		t.notify("Resume is unavailable in --no-session mode")
		return "", false
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(m.tabDirectory(t), file)
	}
	file = sessionPath(file)
	if m.reservedSession(file) {
		t.status = "Session operation in progress."
		return "", false
	}
	stat, err := os.Stat(file)
	if err != nil || !stat.Mode().IsRegular() {
		t.notify("Cannot resume: session file is unavailable")
		return "", false
	}
	return file, true
}
func (m *Model) resume(file string) tea.Cmd {
	file, ok := m.resumeTarget(file)
	if !ok {
		return nil
	}
	for i, other := range m.tabs {
		if !other.dead && sessionPath(other.state.File) == file {
			return m.activate(i)
		}
	}
	t := m.current()
	if t.state.Busy || t.state.Compacting || t.sending || t.awaitingStart || t.editor.Value() != "" || len(t.images) > 0 {
		d := m.newDialog("resume", "Replace this session?", "The saved history remains. Current work and unsent draft will be stopped.")
		d.options = []option{{Label: "Cancel", Value: "no"}, {Label: "Resume selected session", Value: file}}
		t.dialogs = append(t.dialogs, d)
		return m.focus(t)
	}
	return m.replaceSession(t, file)
}
func (m *Model) finishResume(t *tab, file string) tea.Cmd {
	if file == "no" || t != m.current() {
		return nil
	}
	target, ok := m.resumeTarget(file)
	if !ok {
		return nil
	}
	for i, other := range m.tabs {
		if !other.dead && sessionPath(other.state.File) == target {
			return m.activate(i)
		}
	}
	return m.replaceSession(t, target)
}
func (m *Model) replaceSession(old *tab, file string) tea.Cmd {
	return m.replaceSessionCwd(old, file, m.tabDirectory(old))
}

func (m *Model) replaceSessionCwd(old *tab, file, cwd string) tea.Cmd {
	if old == nil || old != m.current() {
		return nil
	}
	if old.mutation != nil || m.reservedSession(file) {
		old.status = "Session operation in progress."
		return nil
	}
	index := m.active
	replacement := m.addTab(file)
	replacement.state.Cwd = cwd
	// addTab centralizes initialization/focus; remove its temporary appended slot
	// before returning to Bubble Tea, so the visible tab count never changes.
	m.tabs = m.tabs[:len(m.tabs)-1]
	m.tabs[index] = replacement
	m.active = index
	focus := m.activate(index)
	client := old.client
	if m.clipboardTarget == old {
		m.clipboardTarget = nil
	}
	closeOld := func() tea.Msg {
		if client != nil {
			_ = client.Close()
		}
		return nil
	}
	// A fresh internal ID isolates old snapshots/events and late start completions.
	// The pool owns both clients, including failures or shutdown during startup.
	return tea.Batch(m.start(replacement), focus, closeOld)
}
