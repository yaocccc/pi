package tooldetail

import (
	"fmt"
	"reflect"
	"strings"

	"pi-goui/internal/session"
)

func (v *renderer) askQuestion(item session.Item, args, details map[string]any) {
	cancelled := details["cancelled"] == true || strings.HasPrefix(strings.TrimSpace(item.Text), "用户取消")
	if cancelled {
		v.line("■ Cancelled", v.theme.Warning)
	} else if (item.Running || !item.HasResult) && details["answer"] == nil && details["questions"] == nil {
		v.line("⋯ Waiting for answer", v.theme.Muted)
	}
	prompts, grouped := args["questions"].([]any)
	answers, answeredGroup := details["questions"].([]any)
	if grouped || answeredGroup {
		if !grouped && args["question"] != nil {
			prompts = []any{args}
		}
		v.heading("Questions")
		if answeredGroup {
			v.line("Answers", v.theme.Muted)
		}
		for i := 0; i < max(len(prompts), len(answers)); i++ {
			if i > 0 {
				v.line(strings.Repeat("─", min(v.width, 24)), v.theme.Muted)
			}
			var prompt, answer any
			if i < len(prompts) {
				prompt = prompts[i]
			}
			if i < len(answers) {
				answer = answers[i]
			}
			v.questionPair(prompt, answer, i, !cancelled)
		}
		if answeredGroup {
			delete(details, "questions")
		}
		// A malformed prompt collection is still useful evidence.
		if _, exists := args["questions"]; exists && !grouped {
			v.heading("Prompt details")
			v.value(args["questions"], "")
		}
	} else {
		v.heading("Question")
		v.questionPair(args, details, -1, !cancelled)
		for key := range details {
			delete(details, key)
		}
	}
	v.output(item.Text)
}

// Join prompt and recorded answer like the transcript question card: render
// the question/options only once, then mark real answers. Conflicting prompt
// fields remain visible separately instead of being overwritten and lost.
func (v *renderer) questionPair(prompt, answer any, index int, active bool) {
	p, pok := prompt.(map[string]any)
	a, aok := answer.(map[string]any)
	if !pok && prompt != nil {
		v.heading("Prompt details")
		v.value(prompt, "")
	}
	if !aok && answer != nil {
		v.heading("Answer details")
		v.value(answer, "")
	}
	q := copyMap(p)
	conflicts := map[string]any{}
	for key, value := range a {
		if previous, exists := q[key]; exists && !reflect.DeepEqual(previous, value) {
			conflicts[key] = previous
		}
		q[key] = value
	}
	v.question(q, index, active && aok)
	if len(conflicts) > 0 {
		v.heading("Original prompt")
		v.value(conflicts, "")
	}
}

func (v *renderer) question(value any, index int, answered bool) {
	question, ok := value.(map[string]any)
	if !ok {
		v.value(value, "")
		return
	}
	question = copyMap(question)
	label := stringField(question, "label")
	header := "◦ Question"
	if index >= 0 {
		header += fmt.Sprintf(" %d", index+1)
	}
	if label != "" {
		header += " · " + label
		delete(question, "label")
	}
	if index >= 0 || label != "" {
		v.line(header, v.theme.Accent)
	}
	if text, ok := question["question"].(string); ok {
		v.line(text, v.theme.Text)
		delete(question, "question")
	}
	if multi, ok := question["multiSelect"].(bool); ok {
		mode := "Single choice"
		if multi {
			mode = "Multiple choices"
		}
		v.line(mode, v.theme.Muted)
		delete(question, "multiSelect")
	}
	selected := questionStrings(question["answer"])
	chosen := map[string]bool{}
	custom := map[string]bool{}
	for _, text := range selected {
		chosen[text] = true
	}
	for _, text := range questionStrings(question["customAnswers"]) {
		custom[text] = true
	}
	if options, ok := question["options"].([]any); ok {
		for _, option := range options {
			text, ok := option.(string)
			if !ok {
				v.value(option, "")
				continue
			}
			color, marker := v.theme.Muted, "○ "
			if answered && chosen[text] && !custom[text] && question["wasCustom"] != true {
				color, marker = v.theme.Success, "✓ "
				delete(chosen, text)
			}
			v.questionOption(marker, text, color)
		}
		delete(question, "options")
	}
	if answered {
		for _, text := range selected {
			if chosen[text] {
				v.questionOption("✓ ", "Custom · "+text, v.theme.Success)
				delete(chosen, text)
			}
		}
	}
	if answer, exists := question["answer"]; exists {
		if answer == nil {
			v.line("⋯ No answer recorded", v.theme.Muted)
		}
		// Keep source fields (including malformed or cancelled answers) for
		// complete inspection; never turn them into an inferred selection.
		v.value(map[string]any{"answer": answer}, "")
		delete(question, "answer")
	} else if answered {
		v.line("⋯ No answer recorded", v.theme.Muted)
	}
	if len(question) > 0 {
		v.value(question, "")
	}
}

// Match the main question card's hanging option gutter without borrowing its
// transcript truncation. Tiny widths drop decoration, not answer text.
func (v *renderer) questionOption(marker, text, color string) {
	if v.width <= 3 {
		v.line(text, color)
		return
	}
	for i, row := range v.hooks.Line(text, color, v.width-2) {
		prefix := "  "
		if i == 0 {
			prefix = strings.Join(v.hooks.Line(marker, color, 2), "")
		}
		v.rows = append(v.rows, prefix+row)
	}
}

func questionStrings(value any) []string {
	switch x := value.(type) {
	case string:
		return []string{x}
	case []any:
		var values []string
		for _, item := range x {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	}
	return nil
}
