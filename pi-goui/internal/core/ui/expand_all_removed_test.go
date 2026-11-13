package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExpandAllShortcutRemoved(t *testing.T) {
	for _, keys := range []string{"", `{"app.tools.expand":"ctrl+o"}`} {
		m := testModel(t, keys)
		tab := m.current()
		tab.setValue("keep draft")
		press(m, 'o', tea.ModCtrl, "")
		if tab.expanded || tab.editor.Value() != "keep draft" {
			t.Fatal("removed expand-all shortcut changed the conversation")
		}
		if strings.Contains(m.welcomeText(), "Expand tools") {
			t.Fatal("welcome still advertises expand-all")
		}
	}
}
