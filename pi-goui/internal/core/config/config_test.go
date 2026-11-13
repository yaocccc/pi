package config

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestLoadNativeOverrides(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	path := filepath.Join(root, "keybindings.json")
	source := `{"app.session.resume":["ctrl+y"],"app.model.select":[],"cursorLeft":"alt+h","tui.editor.cursorLeft":["ctrl+h"],"app.model.cycleBackward":"shift+ctrl+p"}`
	put(t, path, source)
	put(t, filepath.Join(root, "settings.json"), `{"hideThinkingBlock":true,"terminal":{"images":false,"trueColor":true},"defaultTools":["read"],"unknown":{"x":2}}`)
	put(t, filepath.Join(cwd, ".pi", "settings.json"), `{"terminal":{"trueColor":false},"defaultTools":["+bash"]}`)
	c, e := Load(cwd)
	if e != nil {
		t.Fatal(e)
	}
	if !c.Keys.Match("app.session.resume", "ctrl+y") || c.Keys.Match("app.model.select", "ctrl+l") || !c.Keys.Match("tui.editor.cursorLeft", "ctrl+h") || c.Keys.Match("tui.editor.cursorLeft", "alt+h") {
		t.Fatal("override/disable/migration precedence")
	}
	// Like native pi, conflicts are resolved by the caller's action scope, not by deleting another action.
	if !c.Keys.Match("tui.editor.yank", "ctrl+y") {
		t.Fatal("defaults unexpectedly removed")
	}
	if !c.Keys.Match("app.model.cycleBackward", "ctrl+shift+p") {
		t.Fatal("modifier ordering")
	}
	terminal := c.Settings["terminal"].(map[string]any)
	if terminal["images"] != false || terminal["trueColor"] != false {
		t.Fatal(terminal)
	}
	if !reflect.DeepEqual(c.Settings["defaultTools"], []any{"read", "+bash"}) {
		t.Fatal(c.Settings)
	}
	got, _ := os.ReadFile(path)
	if string(got) != source {
		t.Fatal("config rewritten")
	}
	if c.Theme.Accent != "#5fd7ff" || c.Theme.Background != "#262626" {
		t.Fatalf("theme: %+v", c.Theme)
	}
	keys := c.Keys.Keys("app.session.resume")
	keys[0] = "x"
	if !c.Keys.Match("app.session.resume", "ctrl+y") {
		t.Fatal("keys exposed mutable storage")
	}
}
func TestKeyChord(t *testing.T) {
	for _, tc := range []struct {
		k    tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl | tea.ModShift}, "ctrl+shift+p"},
		{tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}, "shift+enter"},
		{tea.KeyPressMsg{Code: '-', Mod: tea.ModCtrl}, "ctrl+-"},
		{tea.KeyPressMsg{Code: 'L', Text: "L"}, "shift+l"},
		{tea.KeyPressMsg{Code: 'l', Text: "L", Mod: tea.ModShift}, "shift+l"},
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, "pagedown"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "escape"},
		{tea.KeyPressMsg{Code: tea.KeyExtended, Text: "ctrl+c"}, ""},
	} {
		if got := KeyChord(tc.k); got != tc.want {
			t.Errorf("%+v: %q != %q", tc.k, got, tc.want)
		}
	}
}
func TestDefaultsAndLoadErrors(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	c, e := Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"tui.input.submit", "tui.editor.cursorLeft", "tui.select.confirm", "tui.altScreen.pageUp", "app.session.new", "app.session.delete", "app.model.select"} {
		if len(c.Keys.Keys(action)) == 0 {
			t.Fatalf("missing supported action: %s", action)
		}
	}
	if !c.Keys.Match("tui.input.newLine", "ctrl+j") {
		t.Fatal("native ctrl+j newline missing")
	}
	put(t, filepath.Join(root, "keybindings.json"), `{"app.model.select":null}`)
	c, e = Load(t.TempDir())
	if e == nil || !c.Keys.Match("app.model.select", "ctrl+l") {
		t.Fatal("invalid binding should report and retain default")
	}
	put(t, filepath.Join(root, "keybindings.json"), `{broken`)
	_, e = Load(t.TempDir())
	if e == nil {
		t.Fatal("invalid JSON should report")
	}
}
func TestEmbeddedThemeAndCustomPi(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", root)
	var v map[string]any
	if e := json.Unmarshal(themeJSON, &v); e != nil {
		t.Fatal(e)
	}
	v["vars"].(map[string]any)["accentBright"] = "#abcdef"
	data, _ := json.Marshal(v)
	put(t, filepath.Join(root, "themes", "pi.json"), string(data))
	c, e := Load(t.TempDir())
	if e != nil || c.Theme.Accent != "#abcdef" {
		t.Fatalf("%+v %v", c.Theme, e)
	}
}

// Installed pi config.js delegates to utils/paths.js normalizePath. In
// particular, ~user and whitespace are NOT expanded/trimmed on Unix.
func TestAgentDirExpansion(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("PI_CODING_AGENT_DIR", "~")
	if AgentDir() != h {
		t.Fatal(AgentDir())
	}
	t.Setenv("PI_CODING_AGENT_DIR", "~/custom")
	if AgentDir() != filepath.Join(h, "custom") {
		t.Fatal(AgentDir())
	}
	for _, p := range []string{"~other/custom", "~ /custom", " ~/custom", "relative/custom"} {
		t.Setenv("PI_CODING_AGENT_DIR", p)
		if AgentDir() != p {
			t.Fatalf("native leaves %q unchanged, got %q", p, AgentDir())
		}
	}
	t.Setenv("PI_CODING_AGENT_DIR", `~\custom`)
	want := `~\custom`
	if filepath.Separator == '\\' {
		want = filepath.Join(h, "custom")
	}
	if AgentDir() != want {
		t.Fatalf("backslash: got %q want %q", AgentDir(), want)
	}
}

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

func TestFrontendThemes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	for _, name := range ThemeNames() {
		want, err := LoadTheme(dir, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := SaveTheme(dir, name); err != nil {
			t.Fatal(err)
		}
		got, err := Load(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got.Theme != want || got.ThemeName != name {
			t.Fatalf("theme %s did not survive reload", name)
		}
	}
	path := filepath.Join(dir, "goui.json")
	before, _ := os.ReadFile(path)
	if SaveTheme(dir, "unknown") == nil {
		t.Fatal("unknown theme accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid theme changed preferences")
	}
}
