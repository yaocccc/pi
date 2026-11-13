package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestResumeThreadsLiveSavedSearchAndTargets(t *testing.T) {
	m := testModel(t, "")
	parent := m.current()
	parent.state.ID = "parent"
	parent.state.File = "/parent.jsonl"
	parent.state.Name = "live parent"
	list := []session.Summary{
		{ID: "child", Path: "/child.jsonl", Name: "child", ParentSession: parent.state.File, Modified: 30, FirstMessage: "needle"},
		{ID: "other", Path: "/other.jsonl", Name: "other", Modified: 20},
		{ID: "parent", Path: parent.state.File, Name: "stale name", Modified: 1},
		{ID: "grand", Path: "/grand.jsonl", Name: "grand", ParentSession: "/child.jsonl", Modified: 40},
	}
	m.sessionDialog(parent, list)
	d := parent.dialogs[0]
	rows := m.filtered(d)
	want := []string{fmt.Sprintf("@%d", parent.id), "/child.jsonl", "/grand.jsonl", "/other.jsonl"}
	for i, r := range rows {
		if r.Value != want[i] {
			t.Fatalf("order %v", rows)
		}
		if r.Data.(session.Summary).Path == "" {
			t.Fatal("target lost")
		}
	}
	if !strings.HasPrefix(rows[1].Label, "↳ ") || !strings.HasPrefix(rows[2].Label, "  ↳ ") || !strings.Contains(rows[0].Label, "live parent") {
		t.Fatalf("labels %v", rows)
	}
	m.width, m.height = 120, 40
	painted := ansi.Strip(strings.Join(m.popupRows(d, m.popupGeometry(d)), "\n"))
	if !strings.Contains(painted, "  ↳ grand") {
		t.Fatalf("lost nested indentation: %s", painted)
	}
	for _, query := range []string{"needle", "child.jsonl"} {
		d.editor.SetValue(query)
		rows = m.filtered(d)
		if len(rows) != 1 || rows[0].Value != "/child.jsonl" || strings.Contains(rows[0].Label, "↳") {
			t.Fatalf("flat search %q: %v", query, rows)
		}
	}
	parent.dialogs = nil
	m.sessionDialog(parent, list, true)
	rows = m.filtered(parent.dialogs[0])
	if rows[0].Value != "/child.jsonl" || strings.Contains(rows[0].Label, "↳") {
		t.Fatalf("hidden parent falsely retained: %v", rows)
	}
}

func TestResumeThreadsNamedFilterAndActionTarget(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	parent, child := actionFile(t, "parent"), actionFile(t, "child")
	list := []session.Summary{{ID: "child", Path: child, Name: "Child", ParentSession: parent, Modified: 30}, {ID: "parent", Path: parent, FirstMessage: "unnamed parent", Modified: 1}}
	m.sessionDialog(tab, list)
	d := tab.dialogs[0]
	d.namedOnly = true
	rows := m.filtered(d)
	if len(rows) != 1 || rows[0].Value != child || strings.Contains(rows[0].Label, "↳") {
		t.Fatalf("filtered parent left false hierarchy: %v", rows)
	}
	d.namedOnly = false
	for i, row := range m.filtered(d) {
		if row.Value == child {
			d.selected = i
		}
	}
	m.requestSessionAction(tab, d, "rename")
	if tab.dialogs[0].action == nil || tab.dialogs[0].action.path != child {
		t.Fatal("threaded selection renamed the wrong session")
	}
}

func TestResumeThreadsOpenChildPreservesDiskParent(t *testing.T) {
	m := testModel(t, "")
	child := m.current()
	child.state.ID = "child"
	child.state.File = "/child.jsonl"
	child.state.Name = "live child"
	list := []session.Summary{{ID: "child", Path: child.state.File, Name: "old child", ParentSession: "/parent.jsonl", Modified: 30}, {ID: "parent", Path: "/parent.jsonl", Name: "parent", Modified: 1}}
	m.sessionDialog(child, list)
	rows := m.filtered(child.dialogs[0])
	if rows[0].Value != "/parent.jsonl" || rows[1].Value != fmt.Sprintf("@%d", child.id) || !strings.HasPrefix(rows[1].Label, "↳ ") {
		t.Fatalf("open child lost relation: %v", rows)
	}
}

func TestRecentsThreadPaintScrollTooltipAndClick(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 40
	parent := m.current()
	parent.state.ID = "parent"
	parent.state.File = actionFile(t, "parent")
	parent.state.Name = "Parent"
	parent.setValue("keep draft")
	child := m.addTab(actionFile(t, "child"))
	child.state.ID = "child"
	child.state.Name = "Child"
	m.activate(0)
	list := []session.Summary{{ID: "child", Path: child.state.File, Name: "old child", ParentSession: parent.state.File, Modified: 9000}, {ID: "parent", Path: parent.state.File, Name: "old parent", Modified: 1000}}
	for i := 0; i < 7; i++ {
		list = append(list, session.Summary{ID: fmt.Sprint(i), Name: fmt.Sprint(i), Path: fmt.Sprintf("/other%d.jsonl", i), Modified: 8000 - int64(i)})
	}
	m.sidebarHistory = sidebarHistoryState{cwd: m.tabDirectory(parent), sessions: list}
	ctx := m.sidebarContext()
	if ctx.History[0] != "Parent" || ctx.History[1] != "Child" || ctx.HistoryDepth[1] != 1 {
		t.Fatalf("context %+v", ctx)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "↳ Child") || strings.Contains(view, "关联分支") {
		t.Fatalf("sidebar %s", view)
	}
	layout := m.sidebarLayout()
	x, y := m.mainWidth()+3, layout.HistoryStart
	m.sidebarHistory.pointerKnown = true
	m.sidebarHistory.pointerX = x
	m.sidebarHistory.pointerY = y
	box, ok := m.sidebarHistoryTooltip(parent)
	if !ok || !strings.Contains(ansi.Strip(strings.Join(box.rows, "\n")), time.UnixMilli(1000).Local().Format("2006-01-02 15:04:05")) {
		t.Fatal("tooltip uses unsorted timestamp")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y + 1})
	if m.current() != child || parent.editor.Value() != "keep draft" {
		t.Fatal("click mapped to wrong row or lost draft")
	}
	m.activate(0)
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: x, Y: y})
	ctx = m.sidebarContext()
	layout = m.sidebarLayout()
	if layout.HistoryOffset != 3 || m.sidebarHistoryAt(layout.HistoryOffset).Name != ctx.History[layout.HistoryOffset] {
		t.Fatal("scroll mapping differs")
	}
	for _, width := range []int{80, 100, 120} {
		m.width = width
		for _, line := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width overflow: %d", width)
			}
		}
	}
}
