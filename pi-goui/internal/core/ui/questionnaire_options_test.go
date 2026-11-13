package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuestionnaireWrappedOptions(t *testing.T) {
	for _, multi := range []bool{false, true} {
		m := testModel(t, "")
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
		tab := m.current()
		label := strings.Repeat("独立设计新挂单模块，保留账户与 signer 的边界。", 3) + "选项末尾"
		payload, _ := json.Marshal(map[string]any{"questions": []map[string]any{{"question": "设计边界", "options": []string{label, "第二项"}, "multiSelect": multi}}})
		raw, _ := json.Marshal(map[string]any{"method": "input", "id": "wrap", "title": "pi-goui:ask-question:v1", "placeholder": string(payload)})
		m.extension(tab, raw)
		if len(tab.dialogs) != 1 {
			t.Fatal("questionnaire not opened")
		}
		d := tab.dialogs[0]
		p := m.popupGeometry(d)
		if len(p.optionRows) <= len(d.options) {
			t.Fatal("long option was not wrapped")
		}
		view := strings.Join(m.popupRows(d, p), "\n")
		if !strings.Contains(ansi.Strip(view), "选项末尾") {
			t.Fatal("option tail was clipped")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) != p.width {
				t.Fatalf("popup width %d, want %d", ansi.StringWidth(line), p.width)
			}
		}
		for n, row := range p.optionRows {
			index, ok := m.popupOptionAt(d, p.x+3, p.optionY+n)
			if !ok || index != row.index {
				t.Fatalf("row %d maps to %d, want %d", n, index, row.index)
			}
		}
		m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyDown}, "")
		if d.selected != 1 {
			t.Fatal("down must move by option, not visual row")
		}
		m.Update(tea.WindowSizeMsg{Width: 46, Height: 18})
		m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyDown}, "")
		p = m.popupGeometry(d)
		found := false
		for _, row := range p.optionRows {
			found = found || row.index == d.selected
		}
		if !found {
			t.Fatal("selected option is not visible after scrolling and resize")
		}
		for _, line := range m.popupRows(d, p) {
			if ansi.StringWidth(line) != p.width {
				t.Fatal("wrapped option exceeds resized popup")
			}
		}
	}
}

func TestQuestionnaireOversizedOptionPaging(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 22})
	tab := m.current()
	label := strings.Repeat("完整阅读这个很长的选项。", 200) + "选项末尾"
	payload, _ := json.Marshal(map[string]any{"questions": []map[string]any{{"question": "请选择", "options": []string{label, "短选项"}}}})
	raw, _ := json.Marshal(map[string]any{"method": "input", "id": "oversize", "title": "pi-goui:ask-question:v1", "placeholder": string(payload)})
	m.extension(tab, raw)
	if len(tab.dialogs) != 1 {
		t.Fatal("questionnaire not opened")
	}
	d := tab.dialogs[0]
	p := m.popupGeometry(d)
	if p.messageTotal <= len(p.message) {
		t.Fatal("oversized option must be available in paged message")
	}
	for i := 0; i < p.messageTotal; i++ {
		m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyPgDown}, "")
	}
	p = m.popupGeometry(d)
	if !strings.Contains(strings.Join(p.message, ""), "选项末尾") {
		t.Fatal("option tail is inaccessible")
	}
	if d.selected != 0 {
		t.Fatal("paging changed selection")
	}
}
