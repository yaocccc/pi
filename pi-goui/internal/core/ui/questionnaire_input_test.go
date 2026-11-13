package ui

import (
	"pi-goui/internal/extensions/hostui"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestQuestionnaireMultilineInput(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	d.selected = 2
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "")
	for _, count := range []int{1, 4, 5, 6, 12} {
		text := strings.Repeat("中文 answer\n", count-1) + "last"
		d.editor.SetValue("")
		m.editTarget(tab, "", tea.PasteMsg{Content: text})
		p := m.popupGeometry(d)
		if p.inputRows != min(count, 5) || d.editor.Area.Height() != p.inputRows {
			t.Fatalf("%d lines: layout=%d editor=%d", count, p.inputRows, d.editor.Area.Height())
		}
		rows := m.popupRows(d, p)
		if strings.Contains(strings.Join(rows, "\n"), "┃") != (count > 5) {
			t.Fatalf("%d lines: scrollbar mismatch", count)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != p.width {
				t.Fatal("popup width changed")
			}
		}
		if d.editor.Value() != text {
			t.Fatal("paste lost text")
		}
	}
	p := m.popupGeometry(d)
	before, _, _ := d.editor.ScrollPosition()
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: p.x + p.width - 1, Y: p.y + 2 + len(p.message)})
	after, _, _ := d.editor.ScrollPosition()
	if after >= before {
		t.Fatalf("wheel did not scroll: %d -> %d", before, after)
	}
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyPgUp}, "pgup")
	offset, _, _ := d.editor.ScrollPosition()
	if offset >= after {
		t.Fatal("page up did not scroll answer")
	}
	text := d.editor.Value()
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "enter")
	if d.extension.Handler.(*hostui.Dialog).Questionnaire.Answers[0].CustomAnswers[0] != text {
		t.Fatal("submit lost multiline answer")
	}
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyRight}, "right")
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyLeft}, "left")
	if d.extension.Handler.(*hostui.Dialog).Questionnaire.Answers[0].CustomAnswers[0] != text {
		t.Fatal("switch lost multiline answer")
	}
}

func TestQuestionnaireInputWrapAndNewline(t *testing.T) {
	m, tab, d := openQuestionnaire(t)
	d.selected = 2
	m.dialogKey(tab, tea.KeyPressMsg{Code: tea.KeyEnter}, "")
	m.dialogKey(tab, tea.KeyPressMsg{Code: 'a', Text: "first"}, "a")
	m.dialogKey(tab, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}, "ctrl+j")
	if d.editor.Value() != "first\n" || m.popupGeometry(d).inputRows != 2 {
		t.Fatalf("newline failed: %q", d.editor.Value())
	}
	if !strings.Contains(strings.Join(m.popupRows(d, m.popupGeometry(d)), "\n"), "换行") {
		t.Fatal("newline guidance missing")
	}
	d.editor.SetValue(strings.Repeat("中文👩‍💻", 90))
	m.resizeDialog(d)
	p := m.popupGeometry(d)
	if p.inputRows != 5 || !strings.Contains(strings.Join(m.popupRows(d, p), "\n"), "┃") {
		t.Fatal("soft wraps did not produce five-row scrolling editor")
	}
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	p = m.popupGeometry(d)
	if p.inputRows > 5 || d.editor.Area.Height() != p.inputRows {
		t.Fatal("resize mismatched editor height")
	}
	if c := m.inputCursor(tab); c == nil || c.Y < p.y+2+len(p.message) || c.Y >= p.y+2+len(p.message)+p.inputRows {
		t.Fatal("cursor outside editor")
	}
}
