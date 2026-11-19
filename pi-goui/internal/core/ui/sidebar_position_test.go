package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
)

func TestSidebarPositionScreenGeometry(t *testing.T) {
	for _, side := range []string{"right", "left"} {
		t.Run(side, func(t *testing.T) {
			m := testModel(t, "")
			m.cfg.SidebarPosition = side
			m.Update(tea.WindowSizeMsg{Width: 132, Height: 40})
			tab := m.current()
			tab.setValue("draft")
			m.resizeEditor(tab)
			sw := m.sidebarWidth()
			mainStart, sideStart, divider, step := 0, m.width-sw, m.width-sw, -4
			if side == "left" {
				mainStart, sideStart, divider, step = sw, 0, sw-1, 4
			}
			v := m.View()
			row := strings.Split(ansi.Strip(v.Content), "\n")[m.editorTop(tab)+1]
			if !strings.Contains(ansi.Cut(row, mainStart, mainStart+m.mainWidth()), "draft") || v.Cursor == nil || v.Cursor.X != mainStart+2+len("draft") {
				t.Fatalf("draft/cursor misplaced on %s: %s %#v", side, row, v.Cursor)
			}
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: divider, Y: 2})
			m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: divider + step, Y: 2})
			m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: divider + step, Y: 2})
			if m.sidebarWidth() != sw+4 || m.panelResize.dragging {
				t.Fatalf("divider drag failed, width=%d, want %d", m.sidebarWidth(), sw+4)
			}
			if side == "right" {
				sideStart -= 4
			}
			start, _ := render.SidebarNewButton(m.sidebarWidth() - 4)
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: sideStart + 2 + start, Y: m.sidebarLayout().NewRow})
			if len(m.tabs) != 2 {
				t.Fatal("new-session button missed after resize")
			}
			m.Update(tea.WindowSizeMsg{Width: 50, Height: 32})
			if m.sidebarWidth() != 0 || m.mainScreenX() != 0 {
				t.Fatal("hidden sidebar displaced main content")
			}
		})
	}
}

func TestResponsiveInspectorAndSidebar(t *testing.T) {
	for _, side := range []string{"left", "right"} {
		t.Run(side, func(t *testing.T) {
			m := inspectorFixture(t)
			m.cfg.SidebarPosition = side
			m.openInspector("write-1")
			m.current().setValue("draft")
			check := func(width, wantSidebar int) {
				t.Helper()
				m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
				v := m.View()
				main, detail, sidebar := m.paneWidths()
				if sidebar != wantSidebar || main+detail+sidebar != width {
					t.Fatalf("%d: unexpected pane widths: %d/%d/%d", width, main, detail, sidebar)
				}
				if width < 60 {
					if !m.inspectorFullscreen() || v.Cursor != nil || detail != 0 {
						t.Fatal("fullscreen inspector")
					}
					return
				}
				origin := 0
				if side == "left" {
					origin = sidebar
				}
				left, w := m.inspectorBounds()
				if detail == 0 || w != detail || left+m.mainScreenX() != origin+main || v.Cursor == nil || v.Cursor.X != origin+7 {
					t.Fatalf("%d: detail/cursor coordinates %d+%d cursor=%v", width, left, w, v.Cursor)
				}
				line := strings.Split(ansi.Strip(v.Content), "\n")[m.editorTop(m.current())+1]
				if !strings.Contains(ansi.Cut(line, origin, origin+main), "draft") || ansi.StringWidth(line) != width {
					t.Fatal("main pane misplaced")
				}
			}
			check(160, 32)
			drag := func(x, target int) {
				m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: 2})
				m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: target, Y: 2})
				m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: target, Y: 2})
			}
			sideX, step := 128, -4
			if side == "left" {
				sideX, step = 31, 4
			}
			drag(sideX, sideX+step)
			if m.sidebarWidth() != 36 || m.detailWidth() != 53 {
				t.Fatal("sidebar divider moved detail")
			}
			detailX := m.mainScreenX() + m.mainWidth()
			drag(detailX, detailX-5)
			if m.sidebarWidth() != 36 || m.detailWidth() != 58 {
				t.Fatal("detail divider moved sidebar")
			}
			check(120, 0)
			check(59, 0)
			check(160, 36)
			start, _ := render.SidebarNewButton(32)
			x := m.mainScreenX() + m.sideX() + 2 + start
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: m.sidebarLayout().NewRow})
			if len(m.tabs) != 2 {
				t.Fatal("visible sidebar did not handle click")
			}
		})
	}
}

func TestLeftInspectorScreenTargets(t *testing.T) {
	m := inspectorFixture(t)
	m.cfg.SidebarPosition = "left"
	r := inspectorTarget(t, m)
	x := m.sidebarWidth() + r.x
	m.Update(tea.MouseMotionMsg{X: x, Y: r.y})
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: r.y})
	if !m.inspectorOpen() {
		t.Fatal("main transcript inspector button missed")
	}
	closeX := m.width - 3
	m.Update(tea.MouseMotionMsg{X: closeX, Y: 1})
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: closeX, Y: 1})
	if m.inspectorOpen() {
		t.Fatal("close on right inspector missed with sidebar on left")
	}
}
