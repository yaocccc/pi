package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestSidebarHistoryTooltip(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	name := strings.Repeat("完整会话名称", 8)
	stamp := time.Date(2026, 3, 2, 14, 30, 0, 0, time.Local)
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(m.current()), sessions: []session.Summary{{Name: name, Modified: stamp.UnixMilli()}}}
	layout := m.sidebarLayout()
	x := m.mainWidth() + 3
	m.Update(tea.MouseMotionMsg{X: x, Y: layout.HistoryStart})
	box, ok := m.sidebarHistoryTooltip(m.current())
	if !ok {
		t.Fatal("hover tooltip missing")
	}
	var displayed strings.Builder
	for _, row := range box.rows[1 : len(box.rows)-1] {
		displayed.WriteString(strings.TrimSpace(ansi.Cut(ansi.Strip(row), 2, box.width-2)))
	}
	if !strings.Contains(displayed.String(), name) || !strings.Contains(displayed.String(), "2026-03-02 14:30:00") {
		t.Fatalf("full wrapped name/time missing: %q", displayed.String())
	}
	if box.x < 0 || box.x+box.width != m.mainWidth()+3 || box.y < 0 || box.y+box.height > m.height {
		t.Fatalf("tooltip out of bounds: %+v", box)
	}
	m.Update(tea.MouseMotionMsg{X: x + 8, Y: layout.HistoryStart})
	moved, visible := m.sidebarHistoryTooltip(m.current())
	if !visible || moved.x != box.x || moved.y != box.y {
		t.Fatal("tooltip followed pointer within the same list item")
	}
	if m.sidebarLayout() != layout {
		t.Fatal("tooltip changed sidebar geometry")
	}
	base := strings.TrimSuffix(strings.Repeat(strings.Repeat("界", m.width/2)+"\n", m.height), "\n")
	for _, row := range strings.Split(m.overlaySidebarHistoryTooltip(m.current(), base), "\n") {
		if ansi.StringWidth(row) != m.width {
			t.Fatal("overlay changed viewport width")
		}
	}
	m.current().dialogs = []*dialog{{}}
	if _, ok := m.sidebarHistoryTooltip(m.current()); ok {
		t.Fatal("tooltip obscures modal")
	}
	m.current().dialogs = nil
	m.Update(tea.MouseMotionMsg{X: 1, Y: layout.HistoryStart})
	if _, ok := m.sidebarHistoryTooltip(m.current()); ok {
		t.Fatal("tooltip remained after pointer left")
	}
}

func TestRecentTooltipDelete(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s := session.Summary{Path: "recent.jsonl", ID: "saved", Name: "Recent"}
	m.sidebarHistory.sessions = []session.Summary{s}
	m.Update(tea.MouseMotionMsg{X: m.mainWidth() + 4, Y: m.sidebarLayout().HistoryStart})
	box, ok := m.sidebarHistoryTooltip(m.current())
	if !ok {
		t.Fatal("missing tooltip")
	}
	r := box.deleteRect()
	base := strings.Repeat(strings.Repeat(" ", m.width)+"\n", m.height)
	visible := func() bool {
		return strings.Contains(ansi.Strip(m.overlaySidebarHistoryTooltip(m.current(), base)), recentDeleteLabel)
	}
	if visible() {
		t.Fatal("button visible before entering corner")
	}
	m.Update(tea.MouseMotionMsg{X: r.x, Y: r.y + 1})
	if !visible() {
		t.Fatal("button missing near corner")
	}
	rows := strings.Split(m.overlaySidebarHistoryTooltip(m.current(), base), "\n")
	right := box.x + m.mainScreenX() + box.width - 1
	if got := ansi.Cut(ansi.Strip(rows[box.y+1]), right-r.width, right); got != recentDeleteLabel {
		t.Fatalf("first interior row, against right border: got %q", got)
	}
	m.Update(tea.MouseClickMsg{X: r.x, Y: r.y, Button: tea.MouseLeft})
	ds := m.current().dialogs
	if len(ds) != 1 || ds[0].action == nil || ds[0].action.path != s.Path || ds[0].action.kind != "delete" {
		t.Fatal("expected confirmation for hovered history")
	}
}

func TestSidebarHistoryTooltipSmallViewportAndOffset(t *testing.T) {
	m := testModel(t, "")
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(m.current()), sessions: []session.Summary{{Name: "first"}, {FirstMessage: "fallback"}}}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 20}, {120, 8}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		layout := m.sidebarLayout()
		m.sidebarHistory.pointerKnown = true
		m.sidebarHistory.pointerX = m.mainWidth() + 3
		m.sidebarHistory.pointerY = layout.HistoryStart + 1
		box, ok := m.sidebarHistoryTooltip(m.current())
		if !ok {
			if size == [2]int{120, 40} {
				t.Fatal("tooltip missing in normal viewport")
			}
			continue
		}
		if box.x < 0 || box.y < 0 || box.x+box.width > m.mainWidth()+3 || box.y+box.height > m.height {
			t.Fatalf("out of bounds at %v", size)
		}
		for _, row := range box.rows {
			if ansi.StringWidth(row) != box.width {
				t.Fatal("tooltip row width differs")
			}
		}
		if !strings.Contains(ansi.Strip(strings.Join(box.rows, "\n")), "fallback") {
			t.Fatal("wrong hovered entry or missing name fallback")
		}
	}
}
