package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func deliverCwdChoices(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, child := range msg {
			deliverCwdChoices(m, child)
		}
	case cwdChoicesMsg:
		m.receiveCwdChoices(msg)
	}
}

func TestCwdCompletionDirectoriesAndPrefixes(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "目录 with spaces", ".hidden", "node_modules"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "not-a-directory"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "alpha"), filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	for _, query := range []string{"", "./", "~/", directoryPrefix(root)} {
		options, err := scanCwdChoices(context.Background(), root, query)
		if err != nil || len(options) != 6 {
			t.Fatalf("query %q: %+v, %v", query, options, err)
		}
		for _, o := range options {
			info, err := os.Stat(o.Value)
			if err != nil || !info.IsDir() || !strings.HasSuffix(o.Value, "/") {
				t.Fatalf("non-directory choice: %+v", o)
			}
		}
	}
	for _, query := range []string{"目录", "./目录", "~/目录", filepath.Join(root, "目录")} {
		options, err := scanCwdChoices(context.Background(), root, query)
		if err != nil || len(options) != 1 || options[0].Value != directoryPrefix(filepath.Join(root, "目录 with spaces")) {
			t.Fatalf("prefix %q: %+v, %v", query, options, err)
		}
	}
}

func TestCwdCompletionTabMouseAndConfirm(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	tab := m.current()
	child := filepath.Join(tab.state.Cwd, "child with spaces")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	deliverCwdChoices(m, m.openCwd(tab))
	d := tab.dialogs[0]
	if options := m.filtered(d); len(options) != 2 || options[0].Label != "child with spaces/" {
		t.Fatalf("initial choices: %+v", options)
	}
	deliverCwdChoices(m, m.key(tea.KeyPressMsg{Code: tea.KeyTab}))
	if d.editor.Value() != directoryPrefix(child) || m.current() != tab || tab.state.Cwd == child {
		t.Fatal("Tab did not complete the directory, or applied it prematurely")
	}
	options := m.filtered(d)
	if len(options) != 1 || options[0].Label != "../" {
		t.Fatalf("empty directory must offer parent: %+v", options)
	}
	p := m.popupGeometry(d)
	deliverCwdChoices(m, m.mouse(tea.Mouse{X: p.x + 3, Y: p.optionY, Button: tea.MouseLeft}))
	if d.editor.Value() != directoryPrefix(tab.state.Cwd) || len(tab.dialogs) != 1 {
		t.Fatal("mouse did not navigate to parent without confirming")
	}
	// Enter applies the input path, NOT the highlighted child suggestion.
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.current() != tab || len(tab.dialogs) != 0 {
		t.Fatal("Enter selected a suggestion instead of confirming the current path")
	}
}

func TestCwdCompletionRejectsStaleResults(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	cmd := m.openCwd(tab)
	d := tab.dialogs[0]
	d.editor.SetValue(filepath.Join(tab.state.Cwd, "new-prefix"))
	deliverCwdChoices(m, cmd)
	if len(m.filtered(d)) != 0 || d.cwd.pending {
		t.Fatal("outdated lookup became selectable or left the dialog stuck")
	}
	cmd = m.ensureCwdCompletion()
	if cmd == nil {
		t.Fatal("updated input did not schedule a fresh lookup")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	deliverCwdChoices(m, cmd)
	if len(tab.dialogs) != 0 || m.current() != tab {
		t.Fatal("late result reopened a cancelled dialog")
	}
}
