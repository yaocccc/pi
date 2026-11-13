package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
	"pi-goui/internal/extensions/worker"
)

type workerPresetSettings = worker.PresetSettings
type workerRoutingSettings = worker.RoutingSettings
type workerSettingsError = worker.SettingsError
type workerModelOption = worker.ModelOption
type workerSettingsRequest = worker.SettingsRequest
type workerSettingsDialog = hostui.WorkerSettings

func (m *Model) openWorkerSettings(d *dialog, payload string) bool {
	h, v, done := m.extensionDialog(nil, d)
	defer done()
	return h.OpenWorkerSettings(v, payload)
}
func (m *Model) refreshWorkerSettings(d *dialog) {
	h, v, done := m.extensionDialog(nil, d)
	defer done()
	h.RefreshWorkerSettings(v)
}
func (m *Model) cancelWorkerSettings(t *tab, d *dialog) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.CancelWorkerSettings(v)
}
func (m *Model) finishWorkerSettings(t *tab, d *dialog, save bool) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.FinishWorkerSettings(v, save)
}
func (m *Model) submitWorkerSettings(t *tab, d *dialog, value string) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.SubmitWorkerSettings(v, value)
}
