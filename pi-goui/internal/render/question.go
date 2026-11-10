package render

import (
	"fmt"
	"strings"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func questionStrings(value any) []string {
	switch value := value.(type) {
	case string:
		if value != "" {
			return []string{value}
		}
	case []any:
		var out []string
		for _, entry := range value {
			if text, ok := entry.(string); ok && text != "" {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

func questionRows(item session.Item, theme config.Theme, width int) []string {
	width = max(1, min(width, 76))
	args, details := visualMap(item.Arguments), visualMap(item.Details)
	prompts, _ := args["questions"].([]any)
	answers, _ := details["questions"].([]any)
	count := max(len(prompts), len(answers))
	grouped := count > 0
	if !grouped {
		count = 1
	}
	cancelled := details["cancelled"] == true || strings.HasPrefix(strings.TrimSpace(item.Text), "用户取消")
	var rows []string
	line := func(marker, text, color string) {
		indent := len([]rune(marker)) + 1
		if width <= indent {
			rows = append(rows, visualWrap(paint(Clean(text), color), width)...)
			return
		}
		for i, row := range visualWrap(paint(Clean(text), color), width-indent) {
			prefix := strings.Repeat(" ", indent)
			if i == 0 {
				prefix = paint(marker, color) + " "
			}
			rows = append(rows, prefix+row)
		}
	}
	if cancelled {
		line("■", "已取消", theme.Warning)
	}
	if item.Error {
		line("✗", workerFirst(item.Text, "提问失败"), theme.Error)
	}
	renderedAnswer := false
	for index := 0; index < count; index++ {
		q := map[string]any{}
		merge := func(value map[string]any) {
			for key, v := range value {
				q[key] = v
			}
		}
		if grouped {
			if index < len(prompts) {
				merge(workerMap(prompts[index]))
			}
			if index < len(answers) {
				merge(workerMap(answers[index]))
			}
		} else {
			merge(args)
			merge(details)
		}
		if index > 0 {
			rows = append(rows, "")
		}
		title := visualString(q, "question")
		if title == "" {
			title = "问题"
		}
		if grouped {
			title = fmt.Sprintf("%d. %s", index+1, title)
		}
		if q["multiSelect"] == true {
			title += "（多选）"
		}
		line("◦", title, theme.Text)
		selected := questionStrings(q["answer"])
		// Older records may have a structured result but no native details.
		if !grouped && q["answer"] == nil && len(details) == 0 {
			result := visualMap([]byte(item.Text))
			selected = questionStrings(result["answer"])
			if len(selected) == 0 {
				selected = questionStrings(result["value"])
			}
		}
		if cancelled || item.Error {
			selected = nil
		}
		options := questionStrings(q["options"])
		chosen := map[string]bool{}
		for _, answer := range selected {
			chosen[answer] = true
		}
		custom := map[string]bool{}
		for _, answer := range questionStrings(q["customAnswers"]) {
			custom[answer] = true
		}
		for _, option := range options {
			marker, color := "○", theme.Muted
			if chosen[option] && !custom[option] && q["wasCustom"] != true {
				marker, color = "✓", theme.Success
				delete(chosen, option)
			}
			line("  "+marker, option, color)
		}
		// Iterate in answer order, not map order, and never duplicate selections.
		for _, answer := range selected {
			if chosen[answer] {
				line("  ✓", "自定义："+answer, theme.Success)
				delete(chosen, answer)
			}
		}
		if len(selected) > 0 {
			renderedAnswer = true
		}
		if !cancelled && !item.Error && len(selected) == 0 && (item.Running || !item.HasResult || q["answer"] == nil && len(details) > 0) {
			line("  ⋯", "等待回答", theme.Muted)
		}
	}
	if !renderedAnswer && !cancelled && !item.Error && item.HasResult && item.Text != "" && len(details) == 0 {
		rows = append(rows, "")
		line("•", item.Text, theme.Text)
	}
	return rows
}
