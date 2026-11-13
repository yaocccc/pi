package hostui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"pi-goui/internal/core/input"
	"strings"
	"testing"
)

func workerSettingsFixture() workerSettingsRequest {
	preset := workerPresetSettings{Model: "test/model", Thinking: "high"}
	return workerSettingsRequest{
		Config: workerRoutingSettings{Version: 2, Fast: preset, Normal: preset, Deep: preset, MaxConcurrentWorkers: 3, DefaultTimeoutMs: 900000, MaxOutputBytes: 65536},
		Models: []workerModelOption{{ID: "test/model", Thinking: []string{"off", "high"}}, {ID: "test/plain", Thinking: []string{"off"}}},
	}
}

func settingsDialog(t *testing.T, request workerSettingsRequest) (DialogHost, *Dialog, *bool) {
	t.Helper()
	editor := input.New()
	d := &Dialog{Editor: &editor}
	closed := false
	h := DialogHost{Resize: func() {}, Close: func(map[string]any) tea.Cmd { closed = true; return nil }}
	raw, _ := json.Marshal(request)
	if !h.OpenWorkerSettings(d, string(raw)) {
		t.Fatal("request not recognized")
	}
	return h, d, &closed
}
func chooseSetting(t *testing.T, h DialogHost, d *Dialog, value string) tea.Cmd {
	t.Helper()
	d.Editor.SetValue("")
	for i, option := range d.Options {
		if option.Value == value {
			d.Selected = i
			return h.SubmitWorkerSettings(d, value)
		}
	}
	t.Fatalf("missing option %q on %s", value, d.WorkerSettings.Page)
	return nil
}
func TestWorkerSettingsGoOwnsFlowAndDirtyCancellation(t *testing.T) {
	request := workerSettingsFixture()
	h, d, closed := settingsDialog(t, request)
	if !strings.Contains(d.Title, "Worker 设置") || d.Kind != "select" {
		t.Fatal("missing local menu")
	}
	chooseSetting(t, h, d, "fast")
	chooseSetting(t, h, d, "model")
	chooseSetting(t, h, d, "test/plain")
	if d.WorkerSettings.Draft.Fast.Thinking != "high" || !strings.Contains(d.Message, "不支持") {
		t.Fatal("model change silently downgraded thinking")
	}
	chooseSetting(t, h, d, "thinking")
	if options := d.Options; len(options) != 1 || options[0].Value != "off" {
		t.Fatal("thinking options not model scoped")
	}
	chooseSetting(t, h, d, "off")
	chooseSetting(t, h, d, "back")
	h.CancelWorkerSettings(d)
	if *closed || d.WorkerSettings.Page != "discard" {
		t.Fatal("dirty Escape must request consent")
	}
	chooseSetting(t, h, d, "no")
	if d.WorkerSettings.Page != "main" || d.WorkerSettings.Draft.Fast.Model != "test/plain" {
		t.Fatal("continue editing lost draft")
	}
	chooseSetting(t, h, d, "cancel")
	chooseSetting(t, h, d, "yes")
	if !*closed {
		t.Fatal("confirmed discard did not close")
	}
}

func TestWorkerSettingsNumericValidationAndEscape(t *testing.T) {
	h, d, closed := settingsDialog(t, workerSettingsFixture())
	chooseSetting(t, h, d, "maxConcurrentWorkers")
	for _, value := range []string{"", "abc", "2.5", "0", "17"} {
		d.Editor.SetValue(value)
		h.SubmitWorkerSettings(d, d.Editor.Value())
		if d.WorkerSettings.Page != "number" || d.WorkerSettings.Draft.MaxConcurrentWorkers != 3 {
			t.Fatal("invalid input changed draft")
		}
	}
	d.Editor.SetValue("4")
	h.SubmitWorkerSettings(d, d.Editor.Value())
	if d.WorkerSettings.Draft.MaxConcurrentWorkers != 4 || d.WorkerSettings.Page != "main" {
		t.Fatal("valid value not applied")
	}
	chooseSetting(t, h, d, "defaultTimeoutMs")
	d.Editor.SetValue("30")
	h.SubmitWorkerSettings(d, d.Editor.Value())
	if d.WorkerSettings.Draft.DefaultTimeoutMs != 30000 {
		t.Fatal("seconds not converted to milliseconds")
	}
	chooseSetting(t, h, d, "maxOutputBytes")
	d.Editor.SetValue("8")
	h.SubmitWorkerSettings(d, d.Editor.Value())
	if d.WorkerSettings.Draft.MaxOutputBytes != 8192 {
		t.Fatal("KiB not converted to bytes")
	}
	chooseSetting(t, h, d, "fast")
	h.CancelWorkerSettings(d)
	if *closed || d.WorkerSettings.Page != "main" {
		t.Fatal("subpage Escape must return, not cancel RPC")
	}
}

func TestWorkerSettingsRetryPreservesOriginalAndShowsLocalError(t *testing.T) {
	request := workerSettingsFixture()
	original := request.Config
	request.Original = &original
	request.Config.MaxConcurrentWorkers = 7
	request.Error = &workerSettingsError{Code: "save-failed", Message: "EACCES"}
	h, d, closed := settingsDialog(t, request)
	if !strings.Contains(d.Message, "保存失败") || !strings.Contains(d.Title, "未保存") {
		t.Fatal("retry did not preserve dirty/error state")
	}
	h.CancelWorkerSettings(d)
	if *closed || d.WorkerSettings.Page != "discard" {
		t.Fatal("retry cancellation must still confirm dirty state")
	}
}
