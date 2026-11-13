package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
)

// Only generic modal state and host operations cross the adapter boundary.
func (m *Model) extensionDialog(t *tab, d *dialog) (hostui.DialogHost, *hostui.Dialog, func()) {
	v := &hostui.Dialog{Kind: d.kind, Title: d.title, Message: d.message, Options: d.options, Selected: d.selected, Editor: &d.editor, Questionnaire: d.questionnaire, WorkerSettings: d.workerSettings}
	commit := func() {
		d.kind, d.title, d.message, d.options, d.selected, d.questionnaire, d.workerSettings = v.Kind, v.Title, v.Message, v.Options, v.Selected, v.Questionnaire, v.WorkerSettings
	}
	h := hostui.DialogHost{
		Resize: func() { commit(); m.resizeDialog(d) },
		Close: func(payload map[string]any) tea.Cmd {
			commit()
			t.dialogs = t.dialogs[1:]
			return tea.Batch(respond(t, d, payload), m.focus(t))
		},
		Page: func() (int, int, int) {
			commit()
			p := m.popupGeometry(d)
			return p.messageStart, p.messageTotal, len(p.message)
		},
		Actions: m.cfg.Keys.Actions, Match: m.cfg.Keys.Match,
	}
	return h, v, commit
}
