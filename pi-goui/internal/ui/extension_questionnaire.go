package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/askquestion"
	"pi-goui/internal/extensions/hostui"
)

type question = askquestion.Question
type questionAnswer = askquestion.Answer
type questionnaire = hostui.Questionnaire

func (m *Model) refreshQuestionnaire(d *dialog) {
	h, v, done := m.extensionDialog(nil, d)
	defer done()
	h.RefreshQuestionnaire(v)
}
func (m *Model) questionnaireKey(t *tab, d *dialog, k tea.KeyPressMsg) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.QuestionnaireKey(v, k)
}
func (m *Model) submitQuestion(t *tab, d *dialog) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.SubmitQuestion(v)
}
func (m *Model) finishQuestionnaire(t *tab, d *dialog) tea.Cmd {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return h.FinishQuestionnaire(v)
}
