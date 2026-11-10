package updates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiReleaseNotesOncePerDisplayedVersion(t *testing.T) {
	root := t.TempDir()
	seen := filepath.Join(t.TempDir(), "frontend", "seen-version")
	put(t, filepath.Join(root, "CHANGELOG.md"), "# Changelog\n\n## [1.2.4] - 2026-01-02\n\n### Fixed\n- New Pi fix.\n\n## [1.2.3] - 2026-01-01\n\n- Previous Pi change.\n")
	inst := installation{Root: root, Version: "1.2.3"}
	notes := loadReleaseNotes(inst, seen)
	if notes.Version != "1.2.3" || !strings.Contains(notes.Markdown, "Previous Pi change") || strings.Contains(notes.Markdown, "New Pi fix") {
		t.Fatalf("wrong installed release: %+v", notes)
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Fatal("reading notes marked them displayed")
	}
	if err := notes.MarkSeen(); err != nil {
		t.Fatal(err)
	}
	if again := loadReleaseNotes(inst, seen); again.Markdown != "" {
		t.Fatal("same Pi version repeated")
	}
	inst.Version = "1.2.4"
	notes = loadReleaseNotes(inst, seen)
	if !strings.Contains(notes.Markdown, "New Pi fix") || strings.Contains(notes.Markdown, "Previous Pi change") {
		t.Fatal("upgraded Pi did not show only its current release")
	}
	if err := notes.MarkSeen(); err != nil {
		t.Fatal(err)
	}
	if again := loadReleaseNotes(inst, seen); again.Markdown != "" {
		t.Fatal("upgrade notice repeated")
	}
}

func TestPiNotesMissingOrUnmatchedStaySilent(t *testing.T) {
	inst := installation{Root: t.TempDir(), Version: "1.2.3"}
	seen := filepath.Join(t.TempDir(), "seen")
	for _, contents := range []string{"", "## [1.2.2]\nWrong release\n", strings.Repeat("x", (2<<20)+1)} {
		put(t, filepath.Join(inst.Root, "CHANGELOG.md"), contents)
		if notes := loadReleaseNotes(inst, seen); notes.Markdown != "" {
			t.Fatal("missing current notes should not fabricate changes")
		}
	}
}

func TestPiNotesUseLocalInstallationEvenOffline(t *testing.T) {
	f := newFixture(t)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PI_OFFLINE", "1")
	f.cfg.Offline = true
	put(t, filepath.Join(f.root, "CHANGELOG.md"), "## 1.2.3\n\nLocal Pi changes.\n")
	put(t, filepath.Join(f.cfg.AgentDir, "installed/CHANGELOG.md"), "Extension changes must not appear.")
	notes := LoadReleaseNotes(f.cfg)
	if notes.Version != "1.2.3" || notes.Markdown != "Local Pi changes." {
		t.Fatalf("wrong notes: %+v", notes)
	}
	noUpdatesRun(t, f)
	if err := notes.MarkSeen(); err != nil {
		t.Fatal(err)
	}
	// Package metadata may change independently of Pi; no repeated Pi notes.
	put(t, filepath.Join(f.cfg.AgentDir, "available.json"), "[]")
	if notes := LoadReleaseNotes(f.cfg); notes.Markdown != "" {
		t.Fatal("extension changes repeated Pi release notes")
	}
	settings, _ := os.ReadFile(filepath.Join(f.cfg.AgentDir, "settings.json"))
	if string(settings) != string(f.settings) {
		t.Fatal("native settings changed")
	}
}
