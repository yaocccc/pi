package hostui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/input"
	"testing"
)

func TestQuestionnaireSummaryRejectsUnanswered(t *testing.T) {
	editor := input.New()
	d := &Dialog{Editor: &editor}
	closed := false
	h := DialogHost{Resize: func() {}, Close: func(map[string]any) tea.Cmd { closed = true; return nil }}
	h.OpenQuestionnaire(d, `{"questions":[{"question":"选择语言","label":"语言","options":["Go","TS"],"multiSelect":true},{"question":"选择模式","label":"模式","options":["快","慢"]}],"review":true}`)
	q := d.Questionnaire
	q.Active = 2
	d.Selected = 2
	h.RefreshQuestionnaire(d)
	h.SubmitQuestion(d)
	if q.Active != 0 || closed {
		t.Fatal("summary accepted incomplete answers")
	}
}
