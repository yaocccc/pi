package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) tabDirectory(t *tab) string {
	if t != nil && t.state.Cwd != "" {
		return t.state.Cwd
	}
	return m.opts.Cwd
}

func (m *Model) cwdEditable(t *tab) bool {
	return t != nil && t.newSession && len(t.state.Items) == 0 && t.state.MessageCount == 0 &&
		len(t.history) == 0 && len(t.state.Steering) == 0 && len(t.state.FollowUp) == 0 &&
		!t.state.Busy && !t.state.Compacting && !t.sending && !t.awaitingStart && !t.interruptRequested &&
		t.imagePending == 0 && m.clipboardTarget != t &&
		t.mutation == nil && !m.reservedSession(t.state.File)
}

func (m *Model) openCwd(t *tab) tea.Cmd {
	if !m.cwdEditable(t) || len(t.dialogs) != 0 {
		return nil
	}
	d := m.newDialog("cwd", "Working directory (cwd)", cwdHelp)
	d.editor.SetValue(directoryPrefix(m.tabDirectory(t)))
	t.dialogs = append(t.dialogs, d)
	return tea.Batch(m.focus(t), m.ensureCwdCompletion())
}

func resolveCwd(value, base string) (string, error) {
	path := strings.TrimSpace(value)
	if path == "" {
		return "", fmt.Errorf("Enter a directory")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Not a directory: %s", path)
	}
	return filepath.Clean(path), nil
}

func (m *Model) finishCwd(t *tab, d *dialog) tea.Cmd {
	if t != m.current() {
		return nil
	}
	if !m.cwdEditable(t) {
		d.message = "Only an empty, idle new session can change its working directory."
		return nil
	}
	cwd, err := resolveCwd(d.editor.Value(), m.tabDirectory(t))
	if err != nil {
		d.message = err.Error()
		return nil
	}
	if cwd == m.tabDirectory(t) {
		t.dialogs = t.dialogs[1:]
		return nil
	}
	cmd := m.replaceSessionCwd(t, "", cwd)
	replacement := m.current()
	if replacement != t {
		// Unsent input is not history. Preserve it, but leave pending events
		// tied to the old tab ID so they cannot affect the replacement.
		replacement.editor = t.editor
		replacement.images = t.images
		replacement.draftRevision = t.draftRevision
		replacement.imageGeneration = t.imageGeneration
		m.resizeEditor(replacement)
	}
	return tea.Batch(cmd, m.focus(replacement), m.requestGit(time.Now()))
}
