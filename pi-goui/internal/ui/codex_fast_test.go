package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
)

func TestCodexFastSidebarConfig(t *testing.T) {
	m := testModel(t, "")
	m.cfg.AgentDir = t.TempDir()
	now := time.Now().Add(time.Second)
	for _, tc := range []struct{ config, level string }{
		{`{"level":"fast"}`, "fast"}, {`{"level":"ultrafast"}`, "ultrafast"}, {`{"level":"normal"}`, "normal"},
		{`{"fast":true}`, "fast"}, {`{"ultrafast":true}`, "ultrafast"}, {`broken`, "ultrafast"}, {`{"level":"bad","fast":true}`, "normal"},
	} {
		if err := os.WriteFile(filepath.Join(m.cfg.AgentDir, "codex-fast.json"), []byte(tc.config), 0600); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
		m.refreshCodexFast(now)
		if m.codexFastMode() != tc.level {
			t.Fatalf("%s: %s", tc.config, m.codexFastMode())
		}
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		lines := strings.Split(ansi.Strip(m.View().Content), "\n")
		line := lines[m.sidebarLayout().ModeRow]
		if !strings.Contains(line, "Mode") || !strings.Contains(line, render.CodexModeLabel(tc.level)) {
			t.Fatalf("missing sidebar Mode: %s", line)
		}
		if strings.Contains(ansi.Strip(m.inputFooter(m.current()).text), " · "+tc.level+" · ") {
			t.Fatal("mode remained in footer")
		}
	}
}

func TestCodexModeDoesNotAnnotateModelName(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	tab := m.current()
	tab.state.Model.ID = "fast-model-ultrafast"
	tab.state.Thinking = "high"
	m.codexFastLabel = "normal"
	before := ansi.Strip(m.inputFooter(tab).text)
	if !strings.Contains(before, "fast-model-ultrafast · high") {
		t.Fatal("model ID must be preserved verbatim")
	}
	for _, level := range codexFastLevels {
		m.codexFastLabel = level
		if got := ansi.Strip(m.inputFooter(tab).text); got != before {
			t.Fatalf("mode %s added a footer annotation: %s", level, got)
		}
	}
}

func TestCodexFastPopupSelection(t *testing.T) {
	m := testModel(t, "")
	m.cfg.AgentDir = t.TempDir()
	m.codexFastLabel = "normal"
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	target, box, ok := m.codexFastGeometry()
	if !ok {
		t.Fatal("missing mode geometry")
	}
	if target.y != m.sidebarLayout().QuotaRow+3 || box.y+box.height != target.y || box.x < m.mainWidth() || box.x+box.width > m.width || box.y < 0 {
		t.Fatalf("popup must be above Mode below Credits within sidebar: target=%+v box=%+v", target, box)
	}
	for _, x := range []int{m.mainWidth() + 2, m.mainWidth() + 5, target.x - 1, target.x + target.width} {
		m.Update(tea.MouseMotionMsg{X: x, Y: target.y})
		if m.codexFastPopupVisible() {
			t.Fatal("label or padding opened popup")
		}
	}
	m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
	if !m.codexFastPopupVisible() {
		t.Fatal("hover did not open popup")
	}
	for _, level := range codexFastLevels {
		if !strings.Contains(ansi.Strip(m.View().Content), render.CodexModeLabel(level)) {
			t.Fatal("missing multiplier label in popup")
		}
	}
	// Move through the adjacent bottom border before entering a selectable row.
	m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y - 1})
	if !m.codexFastPopupVisible() {
		t.Fatal("gap between target and popup")
	}
	before := m.View().Content
	m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 3})
	if !m.codexFastPopupVisible() {
		t.Fatal("popup closed when entering")
	}
	if m.View().Content == before {
		t.Fatal("hover did not highlight option")
	}
	m.Update(tea.MouseWheelMsg{X: box.x + 2, Y: box.y + 3, Button: tea.MouseWheelDown})
	if m.codexFastMode() != "normal" {
		t.Fatal("wheel modified level")
	}
	for i, level := range codexFastLevels {
		target, _, _ = m.codexFastGeometry()
		m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
		m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 1 + i})
		m.Update(tea.MouseClickMsg{X: box.x + 2, Y: box.y + 1 + i, Button: tea.MouseLeft})
		if m.codexFastMode() != level {
			t.Fatalf("click did not set %s", level)
		}
		if m.codexFastPopupVisible() {
			t.Fatal("popup remained open after selection")
		}
		newTarget, newBox, _ := m.codexFastGeometry()
		if newBox != box {
			t.Fatal("popup moved after selecting a different label")
		}
		line := strings.Split(ansi.Strip(m.View().Content), "\n")[newTarget.y]
		if ansi.Cut(line, newTarget.x, newTarget.x+newTarget.width) != render.CodexModeLabel(level) {
			t.Fatal("hit area differs from rendered value")
		}
		path := filepath.Join(m.cfg.AgentDir, "codex-fast.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]string
		if json.Unmarshal(raw, &saved) != nil || saved["level"] != level || len(saved) != 1 {
			t.Fatalf("bad saved config: %s", raw)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("config permissions")
		}
	}
	m.Update(tea.MouseMotionMsg{X: box.x - 1, Y: box.y + 1})
	if m.codexFastPopupVisible() {
		t.Fatal("popup remained outside target")
	}
	// A failed atomic replacement must not change the displayed level or leave temp files.
	file := filepath.Join(m.cfg.AgentDir, "codex-fast.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
	m.Update(tea.MouseMotionMsg{X: box.x + 2, Y: box.y + 1})
	m.Update(tea.MouseClickMsg{X: box.x + 2, Y: box.y + 1, Button: tea.MouseLeft})
	if m.codexFastMode() != "ultrafast" || !strings.Contains(m.current().toast.text, "failed") {
		t.Fatal("failed save changed level or lacked notice")
	}
	files, _ := os.ReadDir(m.cfg.AgentDir)
	if len(files) != 1 {
		t.Fatal("temporary file leaked")
	}
	m.current().dialogs = []*dialog{{}}
	if m.codexFastPopupVisible() {
		t.Fatal("popup visible behind modal")
	}
}
