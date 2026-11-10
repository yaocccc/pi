package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func openQuestionnaire(t *testing.T) (*Model, *tab, *dialog) {
	t.Helper()
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 36})
	tab := m.current()
	payload := `{"questions":[{"question":"选择语言","label":"语言","options":["Go","TS"],"multiSelect":true},{"question":"选择模式","label":"模式","options":["快","慢"]}],"review":true}`
	raw, _ := json.Marshal(map[string]any{"method": "input", "id": "question-1", "title": "pi-goui:ask-question:v1", "placeholder": payload})
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 || tab.dialogs[0].questionnaire == nil {
		t.Fatal("questionnaire RPC not recognized")
	}
	return m, tab, tab.dialogs[0]
}
func TestQuestionnaireFlow(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	q := d.questionnaire
	key := func(code rune) { m.dialogKey(tab, tea.KeyPressMsg{Code: code}, "") }
	if popupFilter(d) || m.popupGeometry(d).inputRows != 0 {
		t.Fatal("questionnaire has filter")
	}
	key('x')
	if d.editor.Value() != "" || len(m.filtered(d)) != 3 {
		t.Fatal("typing filtered choices")
	}
	key(tea.KeyEnter)
	if q.Active != 0 || q.Answers[0].Completed {
		t.Fatal("empty multiselect submitted")
	}
	key(tea.KeySpace)
	key(tea.KeyDown)
	key(tea.KeySpace)
	key(tea.KeyRight)
	key(tea.KeyLeft)
	if len(q.Answers[0].Answers) != 2 {
		t.Fatal("tab switch lost selection")
	}
	key(tea.KeyEnter)
	if q.Active != 1 || !q.Answers[0].Completed || len(tab.dialogs) != 1 {
		t.Fatal("first submit did not advance")
	}
	key(tea.KeyEnter)
	if q.Active != 2 || !q.Answers[1].Completed || len(tab.dialogs) != 1 {
		t.Fatal("missing summary confirmation")
	}
	rows := strings.Join(m.popupRows(d, m.popupGeometry(d)), "\n")
	if !strings.Contains(rows, "总结确认") || !strings.Contains(rows, "Go、TS") {
		t.Fatalf("summary missing answers: %s", rows)
	}
	key(tea.KeyLeft)
	key(tea.KeyDown)
	key(tea.KeyEnter)
	if q.Answers[1].Answers[0] != "慢" || q.Active != 2 {
		t.Fatal("review edit failed")
	}
	key(tea.KeyEnter)
	if len(tab.dialogs) != 0 {
		t.Fatal("final confirmation did not close")
	}
}
func TestQuestionnaireCustomAndCancel(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	q := d.questionnaire
	d.selected = 2
	m.submitQuestion(tab, d)
	if !q.Input || !popupTextOnly(d) {
		t.Fatal("custom editor not opened")
	}
	d.editor.SetValue("Rust")
	m.submitQuestion(tab, d)
	if q.Input || q.Active != 0 || q.Answers[0].Completed {
		t.Fatal("custom multi answer should await submit")
	}
	// Enter submits selected values, including a custom-only answer.
	d.selected = 0
	m.submitQuestion(tab, d)
	if !q.Answers[0].WasCustom || q.Answers[0].Answers[0] != "Rust" {
		t.Fatal("custom answer lost")
	}
	m.questionnaireKey(tab, d, tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(tab.dialogs) != 0 {
		t.Fatal("cancel did not close")
	}
}
func TestQuestionnaireSummaryRejectsUnanswered(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	q := d.questionnaire
	q.Active = 2
	d.selected = 2
	m.refreshQuestionnaire(d)
	m.submitQuestion(tab, d)
	if q.Active != 0 || len(tab.dialogs) != 1 {
		t.Fatal("summary accepted incomplete answers")
	}
}
