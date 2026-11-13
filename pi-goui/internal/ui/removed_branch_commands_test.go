package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRemovedBranchCommandsHaveNoLocalEntry(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	for _, name := range []string{"branches", "branch-tree", "branchs"} {
		if cmd, handled := m.slash(tab, "/"+name); handled || cmd != nil {
			t.Fatalf("removed command %q is still handled locally", name)
		}
		for _, command := range localCommands {
			if command.Name == name {
				t.Fatalf("removed command %q remains in completion", name)
			}
		}
	}
	for _, action := range m.cfg.Keys.Actions() {
		if action == "app.session.branches" || action == "app.session.branchTree" {
			t.Fatalf("removed default action remains: %s", action)
		}
	}
	for _, keep := range []string{"fork", "tree", "origin", "resume", "delete"} {
		found := false
		for _, command := range localCommands {
			found = found || command.Name == keep
		}
		if !found {
			t.Fatalf("unrelated command %q was removed", keep)
		}
	}
}

func TestRemovedBranchKeysAndAliasesCannotOpenViews(t *testing.T) {
	// Old user configuration may remain on disk; it must not revive removed
	// views. The alias names also must no longer map to former native actions.
	for _, bindings := range []string{
		`{"app.session.branches":["f8"],"app.session.branchTree":["f9"]}`,
		`{"branches":["f8"],"branch-tree":["f9"]}`,
	} {
		m := testModel(t, bindings)
		for _, code := range []rune{tea.KeyF8, tea.KeyF9} {
			if cmd := m.key(tea.KeyPressMsg{Code: code}); cmd != nil || len(m.current().dialogs) != 0 {
				t.Fatal("obsolete binding reopened a removed view")
			}
		}
	}
	m := testModel(t, `{"branches":["f8"],"branch-tree":["f9"]}`)
	if len(m.cfg.Keys.Keys("app.session.branches")) != 0 || len(m.cfg.Keys.Keys("app.session.branchTree")) != 0 {
		t.Fatal("removed aliases still resolve to native actions")
	}
}
