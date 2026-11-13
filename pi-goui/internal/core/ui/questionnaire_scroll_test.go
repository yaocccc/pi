package ui

import (
	"encoding/json"
	"pi-goui/internal/extensions/hostui"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuestionnaireLongMessagePaging(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tab := m.current()
	payload, _ := json.Marshal(map[string]any{"questions": []map[string]any{
		{"question": strings.Repeat("这是需要完整阅读的长问题。", 100) + "问题末尾", "label": "长问题", "options": []string{"A", "B"}},
		{"question": "下一题", "label": "下一题", "options": []string{"C"}},
	}})
	raw, _ := json.Marshal(map[string]any{"method": "input", "id": "paging", "title": "pi-goui:ask-question:v1", "placeholder": string(payload)})
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 {
		t.Fatal("questionnaire not opened")
	}
	d := tab.dialogs[0]
	p := m.popupGeometry(d)
	if p.messageTotal <= len(p.message) {
		t.Fatal("expected paged question")
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.popupRows(d, p), "\n")), "PgUp/PgDn") {
		t.Fatal("missing paging hint")
	}
	for i := 0; i < p.messageTotal; i++ {
		m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyPgDown}, "")
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
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyPgUp}, "")
	if m.popupGeometry(d).messageStart >= p.messageStart {
		t.Fatal("page up did not scroll")
	}
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyRight}, "")
	if d.extension.Handler.(*hostui.Dialog).Questionnaire.MessageOffset != 0 || strings.Join(m.popupGeometry(d).message, "") != "下一题" {
		t.Fatal("new question did not reset scroll")
	}
}
