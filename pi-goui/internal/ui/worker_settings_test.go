package ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func workerSettingsFixture() workerSettingsRequest {
	preset := workerPresetSettings{Model: "test/model", Thinking: "high"}
	return workerSettingsRequest{
		Config: workerRoutingSettings{Version: 2, Fast: preset, Normal: preset, Deep: preset, MaxConcurrentWorkers: 3, DefaultTimeoutMs: 900000, MaxOutputBytes: 65536},
		Models: []workerModelOption{{ID: "test/model", Thinking: []string{"off", "high"}}, {ID: "test/plain", Thinking: []string{"off"}}},
	}
}
func openWorkerSettingsTest(t *testing.T, request workerSettingsRequest) (*Model, *tab, *dialog) {
	t.Helper()
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	tab := m.current()
	payload, _ := json.Marshal(request)
	raw, _ := json.Marshal(map[string]any{"type": "extension_ui_request", "id": "settings-1", "method": "input", "title": "pi-goui:worker-settings:v1", "placeholder": string(payload)})
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 || tab.dialogs[0].workerSettings == nil {
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
	t.Fatalf("missing option %q on %s", value, d.workerSettings.page)
	return nil
}

func TestWorkerSettingsGoOwnsFlowAndDirtyCancellation(t *testing.T) {
	request := workerSettingsFixture()
	m, tab, d := openWorkerSettingsTest(t, request)
	if !strings.Contains(d.title, "Worker 设置") || d.kind != "select" {
		t.Fatal("missing local menu")
	}
	chooseSettings(t, m, tab, d, "fast")
	chooseSettings(t, m, tab, d, "model")
	chooseSettings(t, m, tab, d, "test/plain")
	if d.workerSettings.draft.Fast.Thinking != "high" || !strings.Contains(d.message, "不支持") {
		t.Fatal("model change silently downgraded thinking")
	}
	chooseSettings(t, m, tab, d, "thinking")
	if options := m.filtered(d); len(options) != 1 || options[0].Value != "off" {
		t.Fatal("thinking options not model scoped")
	}
	chooseSettings(t, m, tab, d, "off")
	chooseSettings(t, m, tab, d, "back")
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEsc}, "esc")
	if len(tab.dialogs) != 1 || d.workerSettings.page != "discard" {
		t.Fatal("dirty Escape must request consent")
	}
	chooseSettings(t, m, tab, d, "no")
	if d.workerSettings.page != "main" || d.workerSettings.draft.Fast.Model != "test/plain" {
		t.Fatal("continue editing lost draft")
	}
	chooseSettings(t, m, tab, d, "cancel")
	chooseSettings(t, m, tab, d, "yes")
	if len(tab.dialogs) != 0 {
		t.Fatal("confirmed discard did not close")
	}
}

func TestWorkerSettingsNumericValidationAndEscape(t *testing.T) {
	m, tab, d := openWorkerSettingsTest(t, workerSettingsFixture())
	chooseSettings(t, m, tab, d, "maxConcurrentWorkers")
	for _, value := range []string{"", "abc", "2.5", "0", "17"} {
		d.editor.SetValue(value)
		m.confirmDialog(tab, d)
		if d.workerSettings.page != "number" || d.workerSettings.draft.MaxConcurrentWorkers != 3 {
			t.Fatal("invalid input changed draft")
		}
	}
	d.editor.SetValue("4")
	m.confirmDialog(tab, d)
	if d.workerSettings.draft.MaxConcurrentWorkers != 4 || d.workerSettings.page != "main" {
		t.Fatal("valid value not applied")
	}
	chooseSettings(t, m, tab, d, "defaultTimeoutMs")
	d.editor.SetValue("30")
	m.confirmDialog(tab, d)
	if d.workerSettings.draft.DefaultTimeoutMs != 30000 {
		t.Fatal("seconds not converted to milliseconds")
	}
	chooseSettings(t, m, tab, d, "maxOutputBytes")
	d.editor.SetValue("8")
	m.confirmDialog(tab, d)
	if d.workerSettings.draft.MaxOutputBytes != 8192 {
		t.Fatal("KiB not converted to bytes")
	}
	chooseSettings(t, m, tab, d, "fast")
	m.cancelWorkerSettings(tab, d)
	if len(tab.dialogs) != 1 || d.workerSettings.page != "main" {
		t.Fatal("subpage Escape must return, not cancel RPC")
	}
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
	var saved workerRoutingSettings
	if json.Unmarshal([]byte(records[0]["value"].(string)), &saved) != nil || saved.MaxConcurrentWorkers != 5 {
		t.Fatal("saved response lost draft")
	}
	if len(tab.dialogs) != 0 {
		t.Fatal("saved dialog stayed open")
	}
}

func TestWorkerSettingsRetryPreservesOriginalAndShowsLocalError(t *testing.T) {
	request := workerSettingsFixture()
	original := request.Config
	request.Original = &original
	request.Config.MaxConcurrentWorkers = 7
	request.Error = &workerSettingsError{Code: "save-failed", Message: "EACCES"}
	m, tab, d := openWorkerSettingsTest(t, request)
	if !strings.Contains(d.message, "保存失败") || !strings.Contains(d.title, "未保存") {
		t.Fatal("retry did not preserve dirty/error state")
	}
	m.cancelWorkerSettings(tab, d)
	if d.workerSettings.page != "discard" {
		t.Fatal("retry cancellation must still confirm dirty state")
	}
}

func TestWorkerSettingsCleanCancellationAndInvalidRequest(t *testing.T) {
	m, tab, d := openWorkerSettingsTest(t, workerSettingsFixture())
	m.cancelWorkerSettings(tab, d)
	if len(tab.dialogs) != 0 {
		t.Fatal("clean cancel should close directly")
	}
	m.extension(tab, json.RawMessage(`{"type":"extension_ui_request","id":"bad","method":"input","title":"pi-goui:worker-settings:v1","placeholder":"{}"}`))
	if len(tab.dialogs) != 0 {
		t.Fatal("invalid request opened generic raw input")
	}
}
