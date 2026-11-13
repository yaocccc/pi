package ui

import (
	"encoding/json"
	"fmt"
	"pi-goui/internal/extensions/hostui"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func TestSidebarTodoAnimatesAcrossIdleTicks(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 40
	tab := m.current()
	tab.state.Busy = false
	tab.state.Plan = []session.PlanStep{{ID: "1", Goal: "Running", Status: "in_progress"}}
	before := render.Sidebar(tab.state, nil, m.cfg.Theme, 36, 40, m.sidebarContext())
	m.Update(tickMsg(time.Now()))
	after := render.Sidebar(tab.state, nil, m.cfg.Theme, 36, 40, m.sidebarContext())
	if before == after || m.sidebarContext().TodoFrame != m.frame || m.frame == 0 {
		t.Fatal("running task text must follow shared UI ticks even while idle")
	}
	tab.state.Plan = nil
	if len(m.sidebarContext().Todo) != 0 {
		t.Fatal("empty plan should not create todo rows")
	}
}

func TestSidebarTodoCompletionExpiry(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 40
	first := m.current()
	first.state.Plan = []session.PlanStep{{ID: "1", Goal: "Done", Status: "completed"}}
	first.state.PlanRevision++
	started := time.Now()
	cmd := first.syncExtensions()
	if cmd == nil || first.syncExtensions() != nil || m.sidebarLayout().TodoCount != 1 {
		t.Fatal("completed plan must remain visible with one timer")
	}
	msg := cmd()
	if time.Since(started) < 5*time.Second {
		t.Fatal("plan expired before five seconds")
	}
	m.Update(msg)
	if m.sidebarLayout().TodoCount != 0 || len(first.state.Plan) != 1 || first.syncExtensions() != nil {
		t.Fatal("expiry must hide only the display without restarting")
	}
	first.state.Plan[0].Status = "in_progress"
	first.state.PlanRevision++
	if first.syncExtensions() != nil || m.sidebarLayout().TodoCount != 1 {
		t.Fatal("unfinished plan must stay visible without timer")
	}
	m.Update(msg)
	if m.sidebarLayout().TodoCount != 1 {
		t.Fatal("stale timer hid a new plan")
	}
	first.state.Plan[0].Status = "completed"
	first.state.PlanRevision++
	if first.syncExtensions() == nil {
		t.Fatal("new completion needs a fresh timer")
	}
	fresh := hostui.PlanExpired{ID: first.id, Generation: first.extensions[0].(*hostui.Session).Plan.Expiry.Generation}
	m.addTab("")
	second := m.current()
	second.state.Plan = []session.PlanStep{{ID: "2", Goal: "Pending", Status: "pending"}}
	second.state.PlanRevision++
	m.Update(fresh)
	if m.sidebarLayout().TodoCount != 1 {
		t.Fatal("another session's expiry hid this plan")
	}
	m.activate(0)
	if m.sidebarLayout().TodoCount != 0 {
		t.Fatal("background session did not expire")
	}
	first.state.File = "restored-session"
	m.Update(fresh)
	if m.sidebarLayout().TodoCount != 1 || first.extensions[0].(*hostui.Session).Plan.Expiry.Hidden {
		t.Fatal("session restore must show a completed plan again")
	}
}

func TestSidebarTodoScrollAndSessionIsolation(t *testing.T) {
	m := testModel(t, "")
	m.width, m.height = 120, 40
	first := m.current()
	for i := 0; i < 15; i++ {
		first.state.Plan = append(first.state.Plan, session.PlanStep{ID: fmt.Sprint(i), Goal: fmt.Sprint(i), Status: "pending"})
	}
	first.state.PlanRevision = 1
	m.sidebarHistory.cwd = m.tabDirectory(first)
	for i := 0; i < 8; i++ {
		m.sidebarHistory.sessions = append(m.sidebarHistory.sessions, session.Summary{Name: fmt.Sprint(i)})
	}
	before := m.sidebarLayout()
	if before.TodoCount != 10 || before.TodoOffset != 5 {
		t.Fatalf("layout %+v", before)
	}
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: m.mainWidth() + 3, Y: before.TodoStart}
	m.Update(wheel)
	if m.sidebarLayout().TodoOffset != 2 || m.sidebarHistory.offset != 0 || m.sidebarOffset != 0 {
		t.Fatal("todo scroll moved other lists")
	}
	for i := 0; i < 5; i++ {
		m.Update(wheel)
	}
	if m.sidebarLayout().TodoOffset != 0 {
		t.Fatal("todo top clamp")
	}
	wheel.Button = tea.MouseWheelDown
	m.Update(wheel)
	if m.sidebarLayout().TodoOffset != 3 {
		t.Fatal("todo down scroll")
	}
	wheel.Y = before.HistoryStart
	m.Update(wheel)
	if m.sidebarHistory.offset != 3 || m.sidebarLayout().TodoOffset != 3 {
		t.Fatal("history scroll moved todo")
	}
	m.addTab("")
	if m.sidebarLayout().TodoCount != 0 {
		t.Fatal("plan leaked to new session")
	}
	m.activate(0)
	if m.sidebarLayout().TodoOffset != 3 {
		t.Fatal("session lost todo scroll")
	}
	// A genuinely changed plan resets to its last steps; repeated snapshots do not.
	steps, _ := json.Marshal(first.state.Plan)
	event := json.RawMessage(`{"type":"tool_execution_end","toolName":"update_plan","toolCallId":"plan","result":{"details":{"plan":` + string(steps) + `}}}`)
	first.state.Apply(event)
	if m.sidebarLayout().TodoOffset != 3 {
		t.Fatal("identical update reset scroll")
	}
	first.state.Plan[0].Goal = "Changed"
	first.state.PlanRevision++
	if m.sidebarLayout().TodoOffset != 5 {
		t.Fatal("new plan did not follow latest")
	}
	m.height = 18
	if m.sidebarLayout().TodoCount != 0 {
		t.Fatal("short terminal should retain existing controls")
	}
}
