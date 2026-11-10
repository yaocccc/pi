package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
