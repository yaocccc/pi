package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
)

func TestLiveWorkerPreviewCanBeManuallyFolded(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	tab.state.Busy = true
	tab.state.Items = []session.Item{{ID: "w", Role: "tool", ToolName: "worker", Running: true, Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"scout","status":"completed","objective":"Inspect source","result":{"summary":["Saved conclusion"]}}]}`)}}
	m.document(tab)
	tab.follow, tab.offset = false, 0
	clickWorker := func() {
		t.Helper()
		for _, block := range tab.doc.Blocks {
			if block.ID != "w" {
				continue
			}
			click := tea.Mouse{Button: tea.MouseLeft, X: 4, Y: transcriptTop + block.Start}
			m.mouse(click)
			m.mouseRelease(click)
			return
		}
		t.Fatal("missing Worker click target")
	}
	if !strings.Contains(tab.doc.Text, "Saved conclusion") {
		t.Fatal("preview not expanded by default")
	}
	clickWorker()
	if value, ok := tab.folds["w"]; !ok || value || strings.Contains(tab.doc.Text, "Saved conclusion") {
		t.Fatal("Worker click did not collapse preview")
	}
	tab.state.Items[0].Running = false
	m.document(tab)
	if strings.Contains(tab.doc.Text, "Saved conclusion") {
		t.Fatal("completion reopened manually folded Worker")
	}
	clickWorker()
	if !strings.Contains(tab.doc.Text, "Saved conclusion") || !tab.folds["w"] {
		t.Fatal("Worker click did not reopen preview")
	}
	if tab.folds["tool-group:w"] {
		t.Fatal("Worker click expanded outer group")
	}
}

func TestLiveToolPreviewUsesNormalFoldState(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	tab.state.Busy = true
	tab.state.Items = []session.Item{{ID: "call", Role: "tool", ToolName: "bash", Running: true, Arguments: json.RawMessage(`{"command":"pwd"}`), Text: "summary\nLIVE_OUTPUT"}}
	m.document(tab)
	tab.follow, tab.offset = false, 0
	clickTool := func() {
		t.Helper()
		for _, block := range tab.doc.Blocks {
			if block.ID != "call" {
				continue
			}
			click := tea.Mouse{Button: tea.MouseLeft, X: 4, Y: transcriptTop + block.Start}
			m.mouse(click)
			m.mouseRelease(click)
			return
		}
		t.Fatal("missing tool click target")
	}
	if strings.Contains(tab.doc.Text, "LIVE_OUTPUT") {
		t.Fatal("ordinary preview opened by default")
	}
	clickTool()
	if !tab.folds["call"] || !strings.Contains(tab.doc.Text, "LIVE_OUTPUT") {
		t.Fatal("click did not expand normal tool result")
	}
	tab.state.Items[0].Text = "summary\nUPDATED_OUTPUT"
	m.document(tab)
	if !strings.Contains(tab.doc.Text, "UPDATED_OUTPUT") {
		t.Fatal("stream update lost open state")
	}
	tab.folds["tool-group:call"] = true
	m.document(tab)
	if !strings.Contains(tab.doc.Text, "UPDATED_OUTPUT") {
		t.Fatal("full group did not share child fold state")
	}
	tab.folds["tool-group:call"] = false
	m.document(tab)
	clickTool()
	if tab.folds["call"] || strings.Contains(tab.doc.Text, "UPDATED_OUTPUT") {
		t.Fatal("click did not collapse tool preview")
	}
	if tab.folds["tool-group:call"] {
		t.Fatal("child click changed parent fold")
	}
}

func TestRunningAnimationLeavesDocumentStable(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	tab.state.Busy = true
	tab.state.Items = []session.Item{{ID: "call", Role: "tool", ToolName: "bash", Running: true}}
	m.document(tab)
	tab.follow, tab.offset = false, 0
	before := tab.doc.Text
	first := m.View().Content
	m.frame += 3
	second := m.View().Content
	if first == second {
		t.Fatal("view did not animate")
	}
	if tab.doc.Text != before || tab.dirty {
		t.Fatal("animation rebuilt or mutated transcript")
	}
}

func TestLiveToolActivityIndependentOfGroupFold(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	tab.state.Busy = true
	tab.state.Items = []session.Item{{ID: "call", Role: "tool", ToolName: "bash", Running: true}}
	m.document(tab)
	tab.follow, tab.offset = false, 0
	if len(tab.doc.ActivityRows) == 0 {
		t.Fatal("live activity missing")
	}
	group := tab.doc.Blocks[0]
	click := tea.Mouse{Button: tea.MouseLeft, X: 2, Y: transcriptTop + group.Start}
	m.mouse(click)
	m.mouseRelease(click)
	if len(tab.doc.ActivityRows) == 0 || !tab.folds[group.ID] {
		t.Fatal("opening group affected independent activity")
	}
	m.mouse(click)
	m.mouseRelease(click)
	if len(tab.doc.ActivityRows) == 0 || tab.folds[group.ID] {
		t.Fatal("closing group affected independent activity")
	}
	tab.state.Items[0].HasResult = true
	tab.state.Items[0].Running = false
	m.document(tab)
	if len(tab.doc.ActivityRows) == 0 {
		t.Fatal("tool update hid independent activity")
	}
	tab.folds = map[string]bool{}
	tab.state.Busy = false
	m.document(tab)
	if len(tab.doc.ActivityRows) > 0 {
		t.Fatal("idle session showed activity")
	}
}
