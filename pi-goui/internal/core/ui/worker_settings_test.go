package ui

import (
	"pi-goui/internal/extensions/hostui"
	"pi-goui/internal/extensions/worker"

	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func workerSettingsFixture() worker.SettingsRequest {
	preset := worker.PresetSettings{Model: "test/model", Thinking: "high"}
	return worker.SettingsRequest{
		Config: worker.RoutingSettings{Version: 2, Fast: preset, Normal: preset, Deep: preset, MaxConcurrentWorkers: 3, DefaultTimeoutMs: 900000, MaxOutputBytes: 65536},
		Models: []worker.ModelOption{{ID: "test/model", Thinking: []string{"off", "high"}}, {ID: "test/plain", Thinking: []string{"off"}}},
	}
}
func openWorkerSettingsTest(t *testing.T, request worker.SettingsRequest) (*Model, *tab, *dialog) {
	t.Helper()
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	payload, _ := json.Marshal(request)
	raw, _ := json.Marshal(map[string]any{"type": "extension_ui_request", "id": "settings-1", "method": "input", "title": "pi-goui:worker-settings:v1", "placeholder": string(payload)})
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 || tab.dialogs[0].extension.Handler.(*hostui.Dialog).WorkerSettings == nil {
		t.Fatal("request did not open Go settings")
	}
	return m, tab, tab.dialogs[0]
}
func chooseSettings(t *testing.T, m *Model, tab *tab, d *dialog, value string) tea.Cmd {
	t.Helper()
	d.editor.SetValue("")
	for i, option := range m.filtered(d) {
		if option.Value == value {
			d.selected = i
			return m.confirmDialog(tab, d)
		}
	}
	t.Fatalf("missing option %q on %s", value, d.extension.Handler.(*hostui.Dialog).WorkerSettings.Page)
	return nil
}

func TestWorkerSettingsRespondsOnlyOnExplicitSave(t *testing.T) {
	m, tab, d := openWorkerSettingsTest(t, workerSettingsFixture())
	client, wire := fakeClient(t)
	tab.client = client
	chooseSettings(t, m, tab, d, "maxConcurrentWorkers")
	d.editor.SetValue("5")
	commandResult(m.confirmDialog(tab, d))
	if raw, _ := os.ReadFile(wire); strings.Contains(string(raw), "extension_ui_response") {
		t.Fatal("editing prematurely responded")
	}
	commandResult(chooseSettings(t, m, tab, d, "save"))
	records := wireRecords(t, wire, 1)
	if records[0]["type"] != "extension_ui_response" || records[0]["id"] != "settings-1" {
		t.Fatalf("invalid response: %#v", records)
	}
	var saved worker.RoutingSettings
	if json.Unmarshal([]byte(records[0]["value"].(string)), &saved) != nil || saved.MaxConcurrentWorkers != 5 {
		t.Fatal("saved response lost draft")
	}
	if len(tab.dialogs) != 0 {
		t.Fatal("saved dialog stayed open")
	}
}

func TestWorkerSettingsCleanCancellationAndInvalidRequest(t *testing.T) {
	m, tab, _ := openWorkerSettingsTest(t, workerSettingsFixture())
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEsc}, "esc")
	if len(tab.dialogs) != 0 {
		t.Fatal("clean cancel should close directly")
	}
	m.extension(tab, json.RawMessage(`{"type":"extension_ui_request","id":"bad","method":"input","title":"pi-goui:worker-settings:v1","placeholder":"{}"}`))
	if len(tab.dialogs) != 0 {
		t.Fatal("invalid request opened generic raw input")
	}
}
