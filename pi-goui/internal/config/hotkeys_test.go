package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsupportedHotkeysCannotBeEnabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	put(t, filepath.Join(root, "keybindings.json"), `{"app.models.save":"alt+s","app.tree.filter.all":"alt+a","treeFoldOrUp":"alt+f","tui.editor.historyPrevious":"alt+h","tui.editor.pageUp":"alt+p"}`)
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range cfg.Keys.Actions() {
		if strings.HasPrefix(action, "app.models.") || strings.HasPrefix(action, "app.tree.") || strings.HasPrefix(action, "tui.editor.history") || strings.HasPrefix(action, "tui.editor.page") || action == "app.session.deleteNoninvasive" {
			t.Fatalf("unsupported action remains: %s", action)
		}
	}
}

func TestLegacyDeleteBindingMigration(t *testing.T) {
	for _, name := range []string{"app.session.deleteNoninvasive", "deleteSessionNoninvasive"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("PI_CODING_AGENT_DIR", root)
			path := filepath.Join(root, "keybindings.json")
			put(t, path, `{"`+name+`":"alt+d"}`)
			cfg, err := Load(t.TempDir())
			if err != nil || !cfg.Keys.Match("app.session.delete", "alt+d") {
				t.Fatalf("legacy delete not migrated: %v", err)
			}
			put(t, path, `{"`+name+`":"alt+d","app.session.delete":[]}`)
			cfg, err = Load(t.TempDir())
			if err != nil || len(cfg.Keys.Keys("app.session.delete")) != 0 {
				t.Fatalf("explicit delete disable ignored: %v", err)
			}
		})
	}
}
