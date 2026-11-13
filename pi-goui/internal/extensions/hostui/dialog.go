package hostui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/input"
	"pi-goui/internal/extensions/internal/protocol"
)

// OpenExtensionDialog keeps all third-party protocol dispatch and failure text here.
// Unknown/malformed questionnaires retain the host's ordinary input fallback.
func (h DialogHost) OpenExtensionDialog(d *Dialog, title, payload string) string {
	switch protocol.Dialog(title) {
	case protocol.Questionnaire:
		h.OpenQuestionnaire(d, payload)
	case protocol.WorkerSettings:
		if !h.OpenWorkerSettings(d, payload) {
			return "无法打开 Worker 设置：无效数据"
		}
	case protocol.CodexFastSettings:
		if !h.OpenCodexFastSettings(d, payload) {
			return "无法打开 Codex 设置：无效数据"
		}
	}
	return ""
}

type Option = extension.Option

// Dialog is a small view of the host's generic modal, not its application model.
type Dialog struct {
	Kind, Title, Message string
	Options              []Option
	Selected             int
	Editor               *input.Editor
	Questionnaire        *Questionnaire
	WorkerSettings       *WorkerSettings
}
type DialogHost struct {
	Resize  func()
	Close   func(map[string]any) tea.Cmd
	Page    func() (start, total, visible int)
	Actions func() []string
	Match   func(action, key string) bool
}

// MultilineInput identifies the questionnaire's free-form answer editor.
func (d *Dialog) MultilineInput() bool {
	return d.Questionnaire != nil && d.Questionnaire.Input
}

// Unfiltered and InputMode describe presentation without exposing private state.
func (d *Dialog) Unfiltered() bool { return d.Questionnaire != nil }
func (d *Dialog) InputMode() (bool, bool) {
	if d.Questionnaire == nil {
		return false, false
	}
	return d.Questionnaire.Input, true
}
func (d *Dialog) FocusInput() bool     { return d.Questionnaire == nil || d.Questionnaire.Input }
func (d *Dialog) HasLongMessage() bool { return d.Questionnaire != nil }
func (d *Dialog) WrapOptions() bool    { return d.Questionnaire != nil }
func (d *Dialog) MessageRows(message string, width, height int) ([]string, int, int) {
	return d.Questionnaire.MessageRows(message, width, height)
}
func (d *Dialog) Heading(fallback string, width int) string {
	if d.Questionnaire != nil {
		return d.Questionnaire.Tabs(width)
	}
	return fallback
}
func (d *Dialog) Guidance(fallback string, start, total, count int) string {
	if d.Questionnaire != nil {
		return d.Questionnaire.Help(start, total, count)
	}
	return fallback
}
func (d *Dialog) MultipleChoice() bool {
	return d.Questionnaire != nil && d.Questionnaire.ActiveMulti()
}
func (h DialogHost) HandleKey(d *Dialog, key tea.KeyPressMsg) (tea.Cmd, bool) {
	if d.Questionnaire != nil {
		return h.QuestionnaireKey(d, key), true
	}
	return nil, false
}
func (h DialogHost) Cancel(d *Dialog) (tea.Cmd, bool) {
	if d.WorkerSettings != nil {
		return h.CancelWorkerSettings(d), true
	}
	return nil, false
}
func (h DialogHost) ConfirmInput(d *Dialog) (tea.Cmd, bool) {
	if d.Questionnaire != nil {
		return h.SubmitQuestion(d), true
	}
	return nil, false
}
func (h DialogHost) ConfirmOption(d *Dialog, value string) (tea.Cmd, bool) {
	if d.WorkerSettings != nil {
		return h.SubmitWorkerSettings(d, value), true
	}
	return nil, false
}
