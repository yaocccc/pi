package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/session"
)

func largeWorkerItem(count int) session.Item {
	activities := make([]any, count)
	for i := range activities {
		activities[i] = map[string]any{"id": fmt.Sprintf("activity-%d", i), "at": 1700000000000 + i, "type": "tool", "status": "completed", "label": "read", "detail": fmt.Sprintf("源文件/%d.go · 完整内容 👩‍💻\nline with id and at preserved", i)}
	}
	details, _ := json.Marshal(map[string]any{"kind": "worker-ui", "batchId": "large", "tasks": []any{map[string]any{"index": 0, "title": "Large task", "objective": "Inspect all activity", "status": "running", "activities": activities}}})
	return session.Item{ID: "large-worker", Role: "tool", ToolName: "worker", Running: true, Details: details}
}

func BenchmarkWorkerTaskInspector(b *testing.B) {
	for _, count := range []int{200, 1000} {
		for _, action := range []string{"resolve", "scroll-view", "selection-view"} {
			b.Run(fmt.Sprintf("%d/%s", count, action), func(b *testing.B) {
				taskTab := &tab{id: 1}
				taskTab.state.Items = []session.Item{largeWorkerItem(count)}
				m := &Model{tabs: []*tab{taskTab}, width: 160, height: 40, inspector: toolInspector{tabID: 1, itemID: "large-worker", taskKey: "index:large:0"}}
				rows := m.inspectorRows()
				if action == "selection-view" {
					m.inspector.selection = transcriptSelection{dragging: true, lines: rows}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					switch action {
					case "resolve":
						m.inspectedTool()
					case "scroll-view":
						m.inspectorWheel(tea.Mouse{Button: tea.MouseWheelDown, X: 130, Y: 5})
						m.inspectorView()
					case "selection-view":
						m.inspectorMotion(tea.Mouse{Button: tea.MouseLeft, X: 130, Y: 2 + i%30})
						m.inspectorView()
					}
				}
			})
		}
	}
}

func TestWorkerTaskProjectionCacheLegacyUpdates(t *testing.T) {
	item := session.Item{ID: "legacy", Role: "tool", ToolName: "worker", Arguments: json.RawMessage(`{"tasks":[{"taskId":"legacy-task","title":"Before","objective":"Keep objective"}]}`), Text: `{"results":[{"summary":["BEFORE_RESULT"]}]}`}
	var cache inspectorTaskProjection
	selected, title, ok := cache.resolve(item, "id:legacy-task")
	if !ok || title != "Before" || !strings.Contains(string(selected.Details), "BEFORE_RESULT") {
		t.Fatal("legacy task not projected")
	}
	at := strings.Index(string(item.Arguments), "Before")
	copy(item.Arguments[at:], "After!")
	_, title, ok = cache.resolve(item, "id:legacy-task")
	if !ok || title != "After!" {
		t.Fatal("in-place argument update was cached")
	}
	item.Text = `{"results":[{"summary":["AFTER_RESULT"]}]}`
	selected, _, ok = cache.resolve(item, "id:legacy-task")
	if !ok || !strings.Contains(string(selected.Details), "AFTER_RESULT") || strings.Contains(string(selected.Details), "BEFORE_RESULT") {
		t.Fatal("text-only result update was cached")
	}
	item.ToolName = "read"
	if _, _, ok := cache.resolve(item, "id:legacy-task"); ok {
		t.Fatal("changed tool reused Worker projection")
	}
}

func TestWorkerTaskInspectorProjectionCache(t *testing.T) {
	for _, count := range []int{200, 1000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			taskTab := &tab{id: 1}
			taskTab.state.Items = []session.Item{largeWorkerItem(count)}
			m := &Model{tabs: []*tab{taskTab}, width: 160, height: 40, inspector: toolInspector{tabID: 1, itemID: "large-worker", taskKey: "index:large:0"}}
			rows := m.inspectorRows()
			projected, ok := m.inspectedTool()
			if !ok || len(rows) < count {
				t.Fatal("large fixture did not render all activities")
			}
			// Content equality must hit even when a source update supplies a new
			// backing array. Checking allocation count avoids timing thresholds.
			taskTab.state.Items[0].Details = append(json.RawMessage(nil), taskTab.state.Items[0].Details...)
			allocs := testing.AllocsPerRun(50, func() { m.inspectedTool() })
			if allocs != 0 {
				t.Fatalf("unchanged task projection allocated %g times", allocs)
			}
			for i := 0; i < 10; i++ {
				m.inspectorWheel(tea.Mouse{Button: tea.MouseWheelDown, X: 130, Y: 5})
				m.inspector.selection = transcriptSelection{dragging: true, lines: rows}
				m.inspectorMotion(tea.Mouse{Button: tea.MouseLeft, X: 130, Y: 3 + i})
				m.inspectorView()
				m.inspector.selection.dragging = false
			}
			again, _ := m.inspectedTool()
			if &again.Details[0] != &projected.Details[0] || &m.inspectorRows()[0] != &rows[0] {
				t.Fatal("mouse interaction reprojected or rerendered unchanged data")
			}
			// In-place source mutation must invalidate the owned cache snapshot.
			raw := taskTab.state.Items[0].Details
			at := strings.Index(string(raw), "Large task")
			copy(raw[at:], "Other task")
			updated, ok := m.inspectedTool()
			if !ok || m.inspector.taskTitle != "Other task" || &updated.Details[0] == &projected.Details[0] {
				t.Fatal("in-place mutation failed to invalidate task projection")
			}
			if &m.inspectorRows()[0] == &rows[0] {
				t.Fatal("streamed mutation failed to invalidate rendered rows")
			}
			projected = updated
			rows = m.inspectorRows()
			m.width = 90
			if &m.inspectorRows()[0] == &rows[0] {
				t.Fatal("width change retained stale wrapped rows")
			}
			updated, _ = m.inspectedTool()
			if &updated.Details[0] != &projected.Details[0] {
				t.Fatal("resize unnecessarily reprojected source")
			}
			taskTab.state.Items[0].Running = false
			taskTab.state.Items[0].HasResult = true
			updated, _ = m.inspectedTool()
			if updated.Running || !updated.HasResult {
				t.Fatal("completion flags not refreshed")
			}
			m.inspector.taskKey = "index:large:1"
			if m.inspectorOpen() {
				t.Fatal("new identity reused another task's cached data")
			}
			m.inspector.taskKey = "index:large:0"
			if !m.inspectorOpen() {
				t.Fatal("valid task not restored after failed lookup")
			}
			taskTab.state.Items[0].Details = json.RawMessage(`{"kind":"worker-ui","batchId":"large","tasks":[]}`)
			if m.inspectorOpen() || len(m.inspectorRows()) != 0 {
				t.Fatal("removed task retained stale cache")
			}
			taskTab.state.Items[0] = largeWorkerItem(count)
			if !m.inspectorOpen() {
				t.Fatal("negative lookup cache not invalidated")
			}
			m.tabs = append(m.tabs, &tab{id: 2, state: taskTab.state})
			m.active = 1
			if m.inspectorOpen() {
				t.Fatal("task cache leaked into another session")
			}
		})
	}
}

func workerTaskFixture(t *testing.T, running bool) *Model {
	t.Helper()
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	tab := m.current()
	tab.expanded = true
	tab.state.Items = []session.Item{{ID: "w", Role: "tool", ToolName: "worker", Running: running, HasResult: !running, Details: json.RawMessage(`{"kind":"worker-ui","batchId":"batch","snapshotAt":2000,"tasks":[{"index":0,"title":"任务甲 🧪","mode":"review","objective":"ONLY_A_OBJECTIVE","status":"running","activities":[{"type":"tool","status":"running","label":"read","detail":"ONLY_A_ACTIVITY"}],"usage":{"input":100},"future":{"private":"ONLY_A_UNKNOWN"}},{"index":1,"title":"任务乙","mode":"test","objective":"ONLY_B_OBJECTIVE","status":"completed","result":{"summary":["ONLY_B_RESULT"]}}],"payload":{"result":{"results":[{"summary":["BATCH_PRIVATE"]}]}}}`)}}
	m.document(tab)
	tab.follow, tab.offset = false, 0
	return m
}

func workerTaskButton(t *testing.T, m *Model, key string) hitRect {
	t.Helper()
	m.document(m.current())
	id := "w"
	if key != "" {
		id = ""
		for _, block := range m.current().doc.Blocks {
			if block.InspectorTask == key {
				id = block.ID
				break
			}
		}
		if id == "" {
			t.Fatalf("missing task block %q", key)
		}
	}
	for y := 0; y < m.transcriptHeight(m.current()); y++ {
		for x := 0; x < m.mainWidth(); x++ {
			if got, rect, ok := m.toolInspectTarget(x, y); ok && got == id && rect.contains(x, y) {
				return rect
			}
		}
	}
	t.Fatalf("missing button for %q", key)
	return hitRect{}
}

func TestLegacyWorkerTaskViewShowsUnavailable(t *testing.T) {
	m := workerTaskFixture(t, true)
	parent := m.current()
	rect := workerTaskButton(t, m, "index:batch:0")
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y})
	m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y})
	if m.current() != parent || !m.inspectorOpen() || m.inspector.worker != nil || len(m.tabs) != 1 {
		t.Fatal("legacy task did not open isolated summary")
	}
	if !strings.Contains(strings.Join(m.inspectorRows(), "\n"), "简要详情") {
		t.Fatal("missing summary notice")
	}
}

func TestWorkerBodyClickFoldsAndInspectorTakesPriority(t *testing.T) {
	for _, running := range []bool{true, false} {
		m := testModel(t, "")
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		tab := m.current()
		tab.expanded = true
		tab.state.Items = []session.Item{{ID: "w", Role: "tool", ToolName: "worker", Running: running, Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"review","objective":"Check code","status":"completed","result":{"summary":["Looks good"]}}]}`)}}
		m.document(tab)
		tab.follow, tab.offset = false, 0
		rect := inspectorTarget(t, m)
		click := tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y}
		m.mouse(click)
		m.mouseRelease(click)
		if !m.inspectorOpen() || tab.folds["w"] == false && len(tab.folds) > 0 {
			t.Fatalf("inspector click folded worker running=%v", running)
		}
		m.closeInspector()
		m.document(tab)
		found := false
		for _, block := range tab.doc.Blocks {
			if block.Role != "workerTask" {
				continue
			}
			found = true
			click = tea.Mouse{Button: tea.MouseLeft, X: 8, Y: transcriptTop + block.Start}
			m.mouse(click)
			m.mouseRelease(click)
			if expanded, ok := tab.folds["w"]; !ok || expanded {
				t.Fatalf("worker body did not fold running=%v", running)
			}
			break
		}
		if !found {
			t.Fatal("missing task card")
		}
	}
}
