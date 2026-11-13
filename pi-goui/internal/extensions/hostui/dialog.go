package hostui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions"
	"pi-goui/internal/input"
)

// OpenExtensionDialog keeps all third-party protocol dispatch and failure text here.
// Unknown/malformed questionnaires retain the host's ordinary input fallback.
func (h DialogHost) OpenExtensionDialog(d *Dialog, title, payload string) string {
	switch extensions.Dialog(title) {
	case extensions.Questionnaire:
		h.OpenQuestionnaire(d, payload)
	case extensions.WorkerSettings:
		if !h.OpenWorkerSettings(d, payload) {
			return "无法打开 Worker 设置：无效数据"
		}
	case extensions.CodexFastSettings:
		if !h.OpenCodexFastSettings(d, payload) {
			return "无法打开 Codex 设置：无效数据"
		}
	}
	return ""
}

type Option struct {
	Label, Detail, Value string
	Data                 any
}

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
