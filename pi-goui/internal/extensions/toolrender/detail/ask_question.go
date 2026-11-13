package detail

import (
	"fmt"
	"reflect"
	"strings"

	"pi-goui/internal/core/session"
)

func (v *Renderer) AskQuestion(item session.Item, args, details map[string]any) {
	cancelled := details["cancelled"] == true || strings.HasPrefix(strings.TrimSpace(item.Text), "用户取消")
	if cancelled {
		v.Line("■ Cancelled", v.Theme.Warning)
	} else if (item.Running || !item.HasResult) && details["answer"] == nil && details["questions"] == nil {
		v.Line("⋯ Waiting for answer", v.Theme.Muted)
	}
	prompts, grouped := args["questions"].([]any)
	answers, answeredGroup := details["questions"].([]any)
	if grouped || answeredGroup {
		if !grouped && args["question"] != nil {
			prompts = []any{args}
		}
		v.Heading("Questions")
		if answeredGroup {
			v.Line("Answers", v.Theme.Muted)
		}
		for i := 0; i < max(len(prompts), len(answers)); i++ {
			if i > 0 {
				v.Line(strings.Repeat("─", min(v.Width, 24)), v.Theme.Muted)
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
			v.Heading("Prompt details")
			v.Value(args["questions"], "")
		}
	} else {
		v.Heading("Question")
		v.questionPair(args, details, -1, !cancelled)
		for key := range details {
			delete(details, key)
		}
	}
	v.Output(item.Text)
}

// Join prompt and recorded answer like the transcript question card: render
// the question/options only once, then mark real answers. Conflicting prompt
// fields remain visible separately instead of being overwritten and lost.
func (v *Renderer) questionPair(prompt, answer any, index int, active bool) {
	p, pok := prompt.(map[string]any)
	a, aok := answer.(map[string]any)
	if !pok && prompt != nil {
		v.Heading("Prompt details")
		v.Value(prompt, "")
	}
	if !aok && answer != nil {
		v.Heading("Answer details")
		v.Value(answer, "")
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
		v.Heading("Original prompt")
		v.Value(conflicts, "")
	}
}

func (v *Renderer) question(value any, index int, answered bool) {
	question, ok := value.(map[string]any)
	if !ok {
		v.Value(value, "")
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
		v.Line(header, v.Theme.Accent)
	}
	if text, ok := question["question"].(string); ok {
		v.Line(text, v.Theme.Text)
		delete(question, "question")
	}
	if multi, ok := question["multiSelect"].(bool); ok {
		mode := "Single choice"
		if multi {
			mode = "Multiple choices"
		}
		v.Line(mode, v.Theme.Muted)
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
				v.Value(option, "")
				continue
			}
			color, marker := v.Theme.Muted, "○ "
			if answered && chosen[text] && !custom[text] && question["wasCustom"] != true {
				color, marker = v.Theme.Success, "✓ "
				delete(chosen, text)
			}
			v.questionOption(marker, text, color)
		}
		delete(question, "options")
	}
	if answered {
		for _, text := range selected {
			if chosen[text] {
				v.questionOption("✓ ", "Custom · "+text, v.Theme.Success)
				delete(chosen, text)
			}
		}
	}
	if answer, exists := question["answer"]; exists {
		if answer == nil {
			v.Line("⋯ No answer recorded", v.Theme.Muted)
		}
		// Keep source fields (including malformed or cancelled answers) for
		// complete inspection; never turn them into an inferred selection.
		v.Value(map[string]any{"answer": answer}, "")
		delete(question, "answer")
	} else if answered {
		v.Line("⋯ No answer recorded", v.Theme.Muted)
	}
	if len(question) > 0 {
		v.Value(question, "")
	}
}

// Match the main question card's hanging option gutter without borrowing its
// transcript truncation. Tiny widths drop decoration, not answer text.
func (v *Renderer) questionOption(marker, text, color string) {
	if v.Width <= 3 {
		v.Line(text, color)
		return
	}
	for i, row := range v.Hooks.Line(text, color, v.Width-2) {
		prefix := "  "
		if i == 0 {
			prefix = strings.Join(v.Hooks.Line(marker, color, 2), "")
		}
		(*v.Rows) = append((*v.Rows), prefix+row)
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
