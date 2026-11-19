package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendPreferenceDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	cwd := t.TempDir()
	for _, data := range []string{"", `{}`, `{"theme":"nord","unrelated":true}`} {
		if data != "" {
			put(t, filepath.Join(dir, "goui.json"), data)
		}
		cfg, err := Load(cwd)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SidebarPosition != "right" || cfg.ResumeLastSession {
			t.Fatalf("unexpected defaults for %q: %q, %t", data, cfg.SidebarPosition, cfg.ResumeLastSession)
		}
	}
}

func TestSaveFrontendPreferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	cwd := t.TempDir()
	path := filepath.Join(dir, "goui.json")
	put(t, path, `{"theme":"nord","other":{"enabled":true,"n":9007199254740993}}`)
	nativePath := filepath.Join(dir, "settings.json")
	const native = `{"unrelated":"native"}`
	put(t, nativePath, native)
	if err := SaveSidebarPosition(dir, "left"); err != nil {
		t.Fatal(err)
	}
	if err := SaveResumeLastSession(dir, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SidebarPosition != "left" || !cfg.ResumeLastSession || cfg.ThemeName != "gradient-frost" {
		t.Fatalf("preferences did not survive reload: %q, %t, %q", cfg.SidebarPosition, cfg.ResumeLastSession, cfg.ThemeName)
	}

	if err := SaveTheme(dir, "solid-jade"); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SidebarPosition != "left" || !cfg.ResumeLastSession || cfg.ThemeName != "solid-jade" {
		t.Fatalf("saving theme changed other preferences: %q, %t, %q", cfg.SidebarPosition, cfg.ResumeLastSession, cfg.ThemeName)
	}
	if err := SaveSidebarPosition(dir, "right"); err != nil {
		t.Fatal(err)
	}
	if err := SaveResumeLastSession(dir, false); err != nil {
		t.Fatal(err)
	}
	var prefs map[string]json.RawMessage
	if err := readJSON(path, &prefs); err != nil {
		t.Fatal(err)
	}
	if string(prefs["resumeLastSession"]) != "false" || string(prefs["sidebarPosition"]) != `"right"` ||
		!strings.Contains(string(prefs["other"]), "9007199254740993") {
		t.Fatalf("preferences lost values: %v", prefs)
	}
	if data, err := os.ReadFile(nativePath); err != nil || string(data) != native {
		t.Fatalf("native settings were changed: %q (%v)", data, err)
	}
}

func TestInvalidFrontendPreferences(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	cwd := t.TempDir()
	path := filepath.Join(dir, "goui.json")
	const invalid = `{"sidebarPosition":"top","resumeLastSession":true}`
	put(t, path, invalid)
	cfg, err := Load(cwd)
	if err == nil || !strings.Contains(err.Error(), "sidebarPosition") || cfg.SidebarPosition != "right" {
		t.Fatalf("invalid sidebar did not report an error and keep its default: %q (%v)", cfg.SidebarPosition, err)
	}
	if SaveSidebarPosition(dir, "middle") == nil {
		t.Fatal("invalid sidebar position saved")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != invalid {
		t.Fatalf("invalid save changed preferences: %q (%v)", data, err)
	}
	const wrongType = `{"resumeLastSession":"true"}`
	put(t, path, wrongType)
	if _, err := Load(cwd); err == nil || !strings.Contains(err.Error(), "resumeLastSession") {
		t.Fatalf("invalid resume option was not reported: %v", err)
	}
	const broken = `{"other":`
	put(t, path, broken)
	if SaveResumeLastSession(dir, true) == nil {
		t.Fatal("malformed preferences were overwritten")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != broken {
		t.Fatalf("failed save changed malformed preferences: %q (%v)", data, err)
	}
}
