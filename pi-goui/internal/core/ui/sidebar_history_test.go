package ui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"testing"
	"time"
)

func TestSidebarHistoryLoadsAllRecentSessions(t *testing.T) {
	m := testModel(t, "")
	dir := filepath.Join(m.cfg.AgentDir, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		path := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
		data, _ := json.Marshal(map[string]string{"type": "session", "id": fmt.Sprint(i), "cwd": m.tabDirectory(m.current())})
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(i+1), 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	cmd := m.requestSidebarHistory(time.Now())
	if cmd == nil {
		t.Fatal("missing initial history load")
	}
	msg := cmd().(sidebarHistoryMsg)
	if msg.err != nil || len(msg.sessions) != 7 || msg.sessions[0].ID != "6" || msg.sessions[4].ID != "2" {
		t.Fatalf("unexpected recent history: %+v", msg)
	}
	m.Update(msg)
	if len(m.sidebarContext().History) != 7 {
		t.Fatal("history not presented")
	}
	if m.requestSidebarHistory(time.Now()) != nil {
		t.Fatal("history refresh not throttled")
	}
	if m.requestSidebarHistory(time.Now().Add(6*time.Second)) == nil {
		t.Fatal("history not refreshed")
	}
}

func TestSidebarHistoryHeadingDoesNotCollapse(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(m.current()), sessions: []session.Summary{{Name: "Saved"}}}
	before := m.sidebarLayout()
	click := tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + 3, Y: before.HistoryRow}
	m.mouse(click)
	if m.sidebarLayout() != before || m.sidebarLayout().HistoryCount != 1 {
		t.Fatal("heading click changed history layout")
	}
}

func TestSidebarToolbarNewHitbox(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	layout := m.sidebarLayout()
	start, width := render.SidebarNewButton(m.sidebarWidth() - 4)
	x := m.mainWidth() + 2 + start
	before := len(m.tabs)
	for _, outside := range []int{x - 1, x + width} {
		m.mouse(tea.Mouse{Button: tea.MouseLeft, X: outside, Y: layout.NewRow})
		if len(m.tabs) != before {
			t.Fatal("outside toolbar action opened session")
		}
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x + width - 1, Y: layout.NewRow})
	if len(m.tabs) != before+1 {
		t.Fatal("toolbar action did not open session")
	}
}

func TestSidebarHistoryHoverFollowsPointer(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 40
	m.sidebarHistory.cwd = m.tabDirectory(m.current())
	m.current().state.File = "/saved/current.jsonl"
	for i := 0; i < 8; i++ {
		m.sidebarHistory.sessions = append(m.sidebarHistory.sessions, session.Summary{Name: fmt.Sprint(i), Path: "/saved/current.jsonl"})
	}
	if len(m.sidebarContext().HistoryHovered) != 0 {
		t.Fatal("current session highlighted without pointer")
	}
	layout := m.sidebarLayout()
	move := func(x, y int) { m.Update(tea.MouseMotionMsg{X: x, Y: y}) }
	x := m.mainWidth() + 3
	move(x, layout.HistoryStart)
	if h := m.sidebarContext().HistoryHovered; len(h) != 8 || !h[0] {
		t.Fatal("name hover missing")
	}
	move(x, layout.HistoryStart+1)
	if h := m.sidebarContext().HistoryHovered; len(h) != 8 || !h[1] || h[0] {
		t.Fatal("adjacent entry hover missing")
	}
	move(x, layout.HistoryStart)
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: x, Y: layout.HistoryStart})
	if h := m.sidebarContext().HistoryHovered; len(h) != 8 || !h[3] || h[0] {
		t.Fatal("scroll retained stale hover index")
	}
	move(1, layout.HistoryStart)
	if len(m.sidebarContext().HistoryHovered) != 0 {
		t.Fatal("hover remained outside sidebar")
	}
	move(x, layout.HistoryStart)
	m.Update(sidebarHistoryMsg{cwd: m.sidebarHistory.cwd, sessions: nil})
	if len(m.sidebarContext().HistoryHovered) != 0 {
		t.Fatal("refresh retained stale hover")
	}
	if m.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("hover motion events not enabled")
	}
}

func TestSidebarHistoryScrollingAndRefresh(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 38
	for i := 0; i < 12; i++ {
		m.addTab("")
	}
	m.activate(0)
	m.sidebarOffset = 0
	cwd := m.tabDirectory(m.current())
	for i := 0; i < 12; i++ {
		m.sidebarHistory.sessions = append(m.sidebarHistory.sessions, session.Summary{Name: fmt.Sprint(i)})
	}
	m.sidebarHistory.cwd = cwd
	before := m.sidebarLayout()
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: m.mainWidth() + 3, Y: before.HistoryStart + before.HistoryCount*render.SidebarHistoryRows - 1}
	m.Update(wheel)
	if m.sidebarHistory.offset != 3 || m.sidebarOffset != 0 || m.sidebarLayout().NewRow != before.NewRow {
		t.Fatal("history wheel moved open tabs or New session")
	}
	for i := 0; i < 10; i++ {
		m.Update(wheel)
	}
	if m.sidebarHistory.offset != 7 {
		t.Fatal("history did not clamp at bottom")
	}
	wheel.Y = before.NewRow
	m.Update(wheel)
	if m.sidebarOffset != 0 {
		t.Fatal("New session wheel scrolled open tabs")
	}
	wheel.Y = before.SessionsStart
	m.Update(wheel)
	if m.sidebarOffset != 3 || m.sidebarHistory.offset != 7 {
		t.Fatal("open-tab wheel changed history")
	}
	m.Update(sidebarHistoryMsg{cwd: cwd, sessions: m.sidebarHistory.sessions[:6]})
	if m.sidebarHistory.offset != 1 {
		t.Fatal("refresh did not clamp history offset")
	}
	m.Update(sidebarHistoryMsg{cwd: "different", sessions: m.sidebarHistory.sessions})
	if m.sidebarHistory.offset != 0 {
		t.Fatal("directory change did not reset history")
	}
}

func TestSidebarHistoryClickPreservesWorkAndReusesOpenTab(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 36
	path := actionFile(t, "saved")
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(m.current()), offset: 1, sessions: []session.Summary{{Name: "offscreen"}, {Path: path, Name: "Saved"}, {}, {}, {}, {}}}
	current := m.current()
	current.editor.SetValue("unsent draft")
	click := tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + 3, Y: m.sidebarLayout().HistoryStart}
	m.mouse(click)
	if len(current.dialogs) != 0 || len(m.tabs) != 2 || m.current() == current || current.editor.Value() != "unsent draft" || m.current().state.File != sessionPath(path) {
		t.Fatal("history click did not preserve draft in its original tab")
	}
	other := m.current()
	m.activate(0)
	m.mouse(click)
	if m.current() != other || len(m.tabs) != 2 || current.editor.Value() != "unsent draft" {
		t.Fatal("history click did not reuse existing tab")
	}
}
