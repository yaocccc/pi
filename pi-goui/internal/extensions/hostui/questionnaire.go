package hostui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/extensions/askquestion"
	"pi-goui/internal/render"
)

type question = askquestion.Question
type questionAnswer = askquestion.Answer
type Questionnaire struct {
	Questions     []question       `json:"questions"`
	Review        bool             `json:"review"`
	Answers       []questionAnswer `json:"answers"`
	Active        int              `json:"-"`
	Input         bool             `json:"-"`
	MessageOffset int              `json:"-"`
}

func (h DialogHost) OpenQuestionnaire(d *Dialog, payload string) bool {
	var q Questionnaire
	if json.Unmarshal([]byte(payload), &q) != nil || len(q.Questions) == 0 {
		return false
	}
	q.Answers = make([]questionAnswer, len(q.Questions))
	d.Questionnaire = &q
	d.Kind = "questionnaire"
	d.Editor.SetValue("")
	h.RefreshQuestionnaire(d)
	d.Editor.Area.Placeholder = "请输入你的答案"
	return true
}
func (q *Questionnaire) Help(start, total, visible int) string {
	if total > visible {
		return fmt.Sprintf("PgUp/PgDn 翻阅问题 %d–%d/%d · Enter 提交 · Esc 返回", start+1, start+visible, total)
	}
	if q.Input {
		return "Enter 保存答案 · Esc 返回"
	}
	if q.ActiveMulti() {
		return "←→ 切题 · ↑↓ 移动 · 空格勾选 · Enter 提交"
	}
	return "←→ 切题 · ↑↓ 选择 · Enter 提交 · Esc 取消"
}
func (q *Questionnaire) MessageRows(message string, width, height int) (rows []string, start, total int) {
	rows = strings.Split(ansi.Hardwrap(render.Clean(message), width, true), "\n")
	limit := max(1, (height-6)/2)
	total = len(rows)
	start = min(max(0, q.MessageOffset), max(0, total-limit))
	return rows[start:min(total, start+limit)], start, total
}
func (q *Questionnaire) ActiveMulti() bool {
	return q.Active < len(q.Questions) && q.Questions[q.Active].MultiSelect
}
func (q *Questionnaire) Tabs(width int) string {
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
func (h DialogHost) RefreshQuestionnaire(d *Dialog) {
	q := d.Questionnaire
	previousMessage := d.Message
	d.Title = "回答问题"
	d.Options = nil
	if q.Active == len(q.Questions) {
		d.Message = "请检查答案；选择问题可返回修改，选择确认提交完成。"
		for i, question := range q.Questions {
			answer := strings.Join(q.Answers[i].Answers, "、")
			if !q.Answers[i].Completed {
				answer = "未提交"
			}
			d.Options = append(d.Options, Option{Label: fmt.Sprintf("%d. %s：%s", i+1, question.Question, answer)})
		}
		d.Options = append(d.Options, Option{Label: "确认提交"})
		if d.Selected < len(q.Questions) {
			d.Message = q.Questions[d.Selected].Question + "\n" + strings.Join(q.Answers[d.Selected].Answers, "、")
		}
	} else {
		question := q.Questions[q.Active]
		d.Message = question.Question
		for _, value := range question.Options {
			mark := ""
			if question.MultiSelect {
				mark = "[ ] "
				if slices.Contains(q.Answers[q.Active].Answers, value) {
					mark = "[✓] "
				}
			}
			d.Options = append(d.Options, Option{Label: mark + value, Value: value})
		}
		custom := "自己输入…"
		if len(q.Answers[q.Active].CustomAnswers) > 0 {
			custom += "（" + strings.Join(q.Answers[q.Active].CustomAnswers, "、") + "）"
		}
		d.Options = append(d.Options, Option{Label: custom})
	}
	if d.Message != previousMessage {
		q.MessageOffset = 0
	}
	h.Resize()
}
func (h DialogHost) QuestionnaireKey(d *Dialog, k tea.KeyPressMsg) tea.Cmd {
	q := d.Questionnaire
	key := k.String()
	if key == "pgup" || key == "pgdown" {
		start, total, visible := h.Page()
		delta := max(1, visible)
		if key == "pgup" {
			delta = -delta
		}
		q.MessageOffset = min(max(0, start+delta), max(0, total-visible))
		return nil
	}
	if key == "esc" {
		if q.Input {
			q.Input = false
			d.Editor.SetValue("")
			h.RefreshQuestionnaire(d)
			return nil
		}
		return h.Close(map[string]any{"cancelled": true})
	}
	if q.Input {
		if key == "enter" {
			return h.SubmitQuestion(d)
		}
		if !d.Editor.Jumping() {
			for _, action := range h.Actions() {
				if strings.HasPrefix(action, "tui.editor.") && h.Match(action, key) {
					return d.Editor.Update(action, k)
				}
			}
		}
		return d.Editor.Update("", k)
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
		d.Selected = 0
		h.RefreshQuestionnaire(d)
	case "up":
		d.Selected = max(0, d.Selected-1)
	case "down":
		d.Selected = min(len(d.Options)-1, d.Selected+1)
	case "space", " ":
		if q.ActiveMulti() {
			question := q.Questions[q.Active]
			if d.Selected == len(question.Options) {
				q.Input = true
				h.RefreshQuestionnaire(d)
				return nil
			}
			a := &q.Answers[q.Active]
			value := question.Options[d.Selected]
			if i := slices.Index(a.Answers, value); i >= 0 {
				a.Answers = slices.Delete(a.Answers, i, i+1)
			} else {
				a.Answers = append(a.Answers, value)
			}
			a.Completed = false
			h.RefreshQuestionnaire(d)
		}
	case "enter":
		return h.SubmitQuestion(d)
	}
	if q.Active == len(q.Questions) {
		h.RefreshQuestionnaire(d)
	}
	return nil
}
func (h DialogHost) SubmitQuestion(d *Dialog) tea.Cmd {
	q := d.Questionnaire
	if q.Active == len(q.Questions) {
		if d.Selected < len(q.Questions) {
			q.Active = d.Selected
			d.Selected = 0
			h.RefreshQuestionnaire(d)
			return nil
		}
		for i, a := range q.Answers {
			if !a.Completed {
				q.Active = i
				d.Selected = 0
				h.RefreshQuestionnaire(d)
				d.Message += "（请先提交本题）"
				return nil
			}
		}
		return h.FinishQuestionnaire(d)
	}
	question := q.Questions[q.Active]
	a := &q.Answers[q.Active]
	if q.Input {
		value := strings.TrimSpace(d.Editor.Value())
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
		d.Editor.SetValue("")
		if question.MultiSelect {
			d.Selected = 0
			a.Completed = false
			h.RefreshQuestionnaire(d)
			return nil
		}
	} else if d.Selected == len(question.Options) {
		q.Input = true
		h.RefreshQuestionnaire(d)
		return nil
	} else if !question.MultiSelect {
		a.Answers = []string{question.Options[d.Selected]}
		a.CustomAnswers = nil
	}
	if len(a.Answers) == 0 {
		d.Message = question.Question + "（请至少选择一项）"
		return nil
	}
	a.Completed = true
	a.WasCustom = len(a.CustomAnswers) > 0 && len(a.CustomAnswers) == len(a.Answers)
	if len(q.Questions) == 1 && !q.Review {
		return h.FinishQuestionnaire(d)
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
	d.Selected = 0
	if next == len(q.Questions) {
		d.Selected = next
	}
	h.RefreshQuestionnaire(d)
	return nil
}
func (h DialogHost) FinishQuestionnaire(d *Dialog) tea.Cmd {
	value, _ := json.Marshal(struct {
		Answers   []questionAnswer `json:"answers"`
		Cancelled bool             `json:"cancelled"`
	}{Answers: d.Questionnaire.Answers})
	return h.Close(map[string]any{"value": string(value)})
}
