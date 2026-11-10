package ui

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/session"
)

func TestWorkerTaskClicksDoNotFold(t *testing.T) {
	for _, running := range []bool{true, false} {
		m := testModel(t, "")
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		tab := m.current()
		tab.expanded = true
		tab.state.Items = []session.Item{{ID: "w", Role: "tool", ToolName: "worker", Running: running, Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"review","objective":"Check code","status":"completed","result":{"summary":["Looks good"]}}]}`)}}
		m.document(tab)
		tab.follow, tab.offset = false, 0
		before := tab.doc.Text
		found := false
		for _, block := range tab.doc.Blocks {
			if block.Role != "workerTask" {
				continue
			}
			found = true
			for row := block.Start; row < block.End; row++ {
				click := tea.Mouse{Button: tea.MouseLeft, X: 8, Y: transcriptTop + row}
				m.mouse(click)
				m.mouseRelease(click)
				if len(tab.folds) != 0 || tab.doc.Text != before {
					t.Fatalf("task click folded running=%v row=%d", running, row)
				}
			}
		}
		if !found {
			t.Fatal("missing task card")
		}
		for _, block := range tab.doc.Blocks {
			if block.Role != "toolGroup" {
				continue
			}
			click := tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + block.Start}
			m.mouse(click)
			m.mouseRelease(click)
			if expanded, ok := tab.folds[block.ID]; !ok || expanded {
				t.Fatal("calls group stopped responding to clicks")
			}
			break
		}
	}
}
