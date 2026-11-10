package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
)

type question struct {
	Question    string   `json:"question"`
	Label       string   `json:"label"`
	Options     []string `json:"options"`
	MultiSelect bool     `json:"multiSelect"`
}
type questionAnswer struct {
	Answers       []string `json:"answers"`
	CustomAnswers []string `json:"customAnswers,omitempty"`
	WasCustom     bool     `json:"wasCustom"`
	Completed     bool     `json:"completed"`
}
type questionnaire struct {
	Questions []question       `json:"questions"`
	Review    bool             `json:"review"`
	Answers   []questionAnswer `json:"answers"`
	Active    int              `json:"-"`
	Input     bool             `json:"-"`
}

func (q *questionnaire) activeMulti() bool {
	return q.Active < len(q.Questions) && q.Questions[q.Active].MultiSelect
}
func (q *questionnaire) tabs(width int) string {
	labels := make([]string, len(q.Questions))
	for i, question := range q.Questions {
		label := question.Label
		if label == "" {
			label = fmt.Sprintf("问题 %d", i+1)
		}
		label = ansi.Truncate(render.Clean(strings.Join(strings.Fields(label), " ")), 14, "…")
		if q.Answers[i].Completed {
			label += " ✓"
		}
		labels[i] = label
	}
	if q.Review || len(q.Questions) > 1 {
		labels = append(labels, "总结确认")
	}
	labels[q.Active] = "[" + labels[q.Active] + "]"
	// Keep the active tab visible even in narrow terminals.
	start := 0
	for start < q.Active && ansi.StringWidth(strings.Join(labels[start:q.Active+1], " | ")) > width {
		start++
	}
	title := strings.Join(labels[start:], " | ")
	if start > 0 {
		title = "‹ " + title
	}
	return ansi.Truncate(title, width, "…")
}
func (m *Model) refreshQuestionnaire(d *dialog) {
	q := d.questionnaire
	d.title = "回答问题"
	d.options = nil
	if q.Active == len(q.Questions) {
		d.message = "请检查答案；选择问题可返回修改，选择确认提交完成。"
		for i, question := range q.Questions {
			answer := strings.Join(q.Answers[i].Answers, "、")
			if !q.Answers[i].Completed {
				answer = "未提交"
			}
			d.options = append(d.options, option{Label: fmt.Sprintf("%d. %s：%s", i+1, question.Question, answer)})
		}
		d.options = append(d.options, option{Label: "确认提交"})
		if d.selected < len(q.Questions) {
			d.message = q.Questions[d.selected].Question + "\n" + strings.Join(q.Answers[d.selected].Answers, "、")
		}
	} else {
		question := q.Questions[q.Active]
		d.message = question.Question
		for _, value := range question.Options {
			mark := ""
			if question.MultiSelect {
				mark = "[ ] "
				if slices.Contains(q.Answers[q.Active].Answers, value) {
					mark = "[✓] "
				}
			}
			d.options = append(d.options, option{Label: mark + value, Value: value})
		}
		custom := "自己输入…"
		if len(q.Answers[q.Active].CustomAnswers) > 0 {
			custom += "（" + strings.Join(q.Answers[q.Active].CustomAnswers, "、") + "）"
		}
		d.options = append(d.options, option{Label: custom})
	}
	m.resizeDialog(d)
}
func (m *Model) questionnaireKey(t *tab, d *dialog, k tea.KeyPressMsg) tea.Cmd {
	q := d.questionnaire
	key := k.String()
	if key == "esc" {
		if q.Input {
			q.Input = false
			d.editor.SetValue("")
			m.refreshQuestionnaire(d)
			return nil
		}
		t.dialogs = t.dialogs[1:]
		return tea.Batch(respond(t, d, map[string]any{"cancelled": true}), m.focus(t))
	}
	if q.Input {
		if key == "enter" {
			return m.submitQuestion(t, d)
		}
		return d.editor.Update("", k)
	}
	switch key {
	case "left", "right":
		count := len(q.Questions)
		if q.Review || count > 1 {
			count++
		}
		delta := 1
		if key == "left" {
			delta = -1
		}
		q.Active = (q.Active + delta + count) % count
		d.selected = 0
		m.refreshQuestionnaire(d)
	case "up":
		d.selected = max(0, d.selected-1)
	case "down":
		d.selected = min(len(d.options)-1, d.selected+1)
	case "space", " ":
		if q.activeMulti() {
			question := q.Questions[q.Active]
			if d.selected == len(question.Options) {
				q.Input = true
				m.refreshQuestionnaire(d)
				return nil
			}
			a := &q.Answers[q.Active]
			value := question.Options[d.selected]
			if i := slices.Index(a.Answers, value); i >= 0 {
				a.Answers = slices.Delete(a.Answers, i, i+1)
			} else {
				a.Answers = append(a.Answers, value)
			}
			a.Completed = false
			m.refreshQuestionnaire(d)
		}
	case "enter":
		return m.submitQuestion(t, d)
	}
	if q.Active == len(q.Questions) {
		m.refreshQuestionnaire(d)
	}
	return nil
}
func (m *Model) submitQuestion(t *tab, d *dialog) tea.Cmd {
	q := d.questionnaire
	if q.Active == len(q.Questions) {
		if d.selected < len(q.Questions) {
			q.Active = d.selected
			d.selected = 0
			m.refreshQuestionnaire(d)
			return nil
		}
		for i, a := range q.Answers {
			if !a.Completed {
				q.Active = i
				d.selected = 0
				m.refreshQuestionnaire(d)
				d.message += "（请先提交本题）"
				return nil
			}
		}
		return m.finishQuestionnaire(t, d)
	}
	question := q.Questions[q.Active]
	a := &q.Answers[q.Active]
	if q.Input {
		value := strings.TrimSpace(d.editor.Value())
		if value == "" {
			return nil
		}
		if !question.MultiSelect {
			a.Answers = nil
			a.CustomAnswers = nil
		}
		if !slices.Contains(a.Answers, value) {
			a.Answers = append(a.Answers, value)
			a.CustomAnswers = append(a.CustomAnswers, value)
		}
		q.Input = false
		d.editor.SetValue("")
		if question.MultiSelect {
			d.selected = 0
			a.Completed = false
			m.refreshQuestionnaire(d)
			return nil
		}
	} else if d.selected == len(question.Options) {
		q.Input = true
		m.refreshQuestionnaire(d)
		return nil
	} else if !question.MultiSelect {
		a.Answers = []string{question.Options[d.selected]}
		a.CustomAnswers = nil
	}
	if len(a.Answers) == 0 {
		d.message = question.Question + "（请至少选择一项）"
		return nil
	}
	a.Completed = true
	a.WasCustom = len(a.CustomAnswers) > 0 && len(a.CustomAnswers) == len(a.Answers)
	if len(q.Questions) == 1 && !q.Review {
		return m.finishQuestionnaire(t, d)
	}
	// Submit one question at a time, then review the entire questionnaire.
	next := len(q.Questions)
	for i := 1; i <= len(q.Questions); i++ {
		j := (q.Active + i) % len(q.Questions)
		if !q.Answers[j].Completed {
			next = j
			break
		}
	}
	q.Active = next
	d.selected = 0
	if next == len(q.Questions) {
		d.selected = next
	}
	m.refreshQuestionnaire(d)
	return nil
}
func (m *Model) finishQuestionnaire(t *tab, d *dialog) tea.Cmd {
	value, _ := json.Marshal(struct {
		Answers   []questionAnswer `json:"answers"`
		Cancelled bool             `json:"cancelled"`
	}{Answers: d.questionnaire.Answers})
	t.dialogs = t.dialogs[1:]
	return tea.Batch(respond(t, d, map[string]any{"value": string(value)}), m.focus(t))
}
