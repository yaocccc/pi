package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuestionnaireLongMessagePaging(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tab := m.current()
	d := m.newDialog("questionnaire", "", "")
	d.questionnaire = &questionnaire{
		Questions: []question{{Question: strings.Repeat("这是需要完整阅读的长问题。", 100) + "问题末尾", Options: []string{"A", "B"}}, {Question: "下一题", Options: []string{"C"}}},
		Answers:   make([]questionAnswer, 2),
	}
	m.refreshQuestionnaire(d)
	p := m.popupGeometry(d)
	if p.messageTotal <= len(p.message) {
		t.Fatal("expected paged question")
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.popupRows(d, p), "\n")), "PgUp/PgDn") {
		t.Fatal("missing paging hint")
	}
	for i := 0; i < p.messageTotal; i++ {
		m.questionnaireKey(tab, d, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	p = m.popupGeometry(d)
	if !strings.Contains(strings.Join(p.message, ""), "问题末尾") {
		t.Fatal("question end is inaccessible")
	}
	if d.selected != 0 {
		t.Fatal("paging changed selection")
	}
	for n := 0; n < p.count; n++ {
		if index, ok := m.popupOptionAt(d, p.x+3, p.optionY+n); !ok || index != p.start+n {
			t.Fatal("option hit geometry mismatch")
		}
	}
	m.questionnaireKey(tab, d, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.popupGeometry(d).messageStart >= p.messageStart {
		t.Fatal("page up did not scroll")
	}
	m.questionnaireKey(tab, d, tea.KeyPressMsg{Code: tea.KeyRight})
	if d.questionnaire.MessageOffset != 0 || strings.Join(m.popupGeometry(d).message, "") != "下一题" {
		t.Fatal("new question did not reset scroll")
	}
}
