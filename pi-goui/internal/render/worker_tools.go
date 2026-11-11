package render

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/config"
)

var workerThinkingHeading = regexp.MustCompile(`\*\*[^*]+\*\*`)

// Native snapshots flatten whitespace, so consecutive bold thinking headings
// can arrive in one activity. Recover those boundaries before removing markup.
func workerThinkingSteps(text string) []string {
	text = Clean(text)
	headings := workerThinkingHeading.FindAllStringIndex(text, -1)
	var chunks []string
	if len(headings) == 0 {
		chunks = []string{text}
	} else {
		chunks = append(chunks, text[:headings[0][0]])
		for i, heading := range headings {
			end := len(text)
			if i+1 < len(headings) {
				end = headings[i+1][0]
			}
			chunks = append(chunks, text[heading[0]:end])
		}
	}
	var steps []string
	for _, chunk := range chunks {
		for _, line := range strings.Split(strings.ReplaceAll(chunk, "**", ""), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				steps = append(steps, line)
			}
		}
	}
	return steps
}

// Tool totals are supplied by Worker. Thinking counts include only retained
// snapshot text; discarded historical thinking is never guessed.
func workerToolRows(card workerCard, theme config.Theme, width int) []string {
	type step struct {
		activity map[string]any
		thinking string
	}
	var steps []step
	tools, thoughts := 0, 0
	for _, value := range card.activities {
		activity := workerMap(value)
		switch visualString(activity, "type") {
		case "tool":
			steps = append(steps, step{activity: activity})
			tools++
		case "thinking":
			for _, text := range workerThinkingSteps(visualString(activity, "detail")) {
				steps = append(steps, step{thinking: text})
				thoughts++
			}
		}
	}
	total := max(card.toolCalls, tools) + thoughts
	if total == 0 {
		return nil
	}
	indent := min(2, max(0, width-1))
	prefix := strings.Repeat(" ", indent)
	available := max(1, width-indent)
	header := paint("≡", theme.Muted) + " " + paint("Activity", theme.Text)
	rows := []string{prefix + fit(header, available)}
	indent = min(4, max(0, width-1))
	prefix = strings.Repeat(" ", indent)
	available = min(60, max(1, width-indent))
	shown := min(3, len(steps))
	if hidden := total - shown; hidden > 0 {
		rows = append(rows, prefix+fit(paint(fmt.Sprintf("… %d items (已折叠)", hidden), theme.Muted), available))
	}
	for _, step := range steps[len(steps)-shown:] {
		if step.thinking != "" {
			rows = append(rows, prefix+fit(paint("!", theme.Thinking)+" "+paint(oneLine(step.thinking), theme.Muted), available))
			continue
		}
		activity := step.activity
		icon, color := workerStatus(visualString(activity, "status"), theme)
		text := paint(icon, color) + " " + paint(oneLine(workerFirst(visualString(activity, "label"), "tool")), theme.Text)
		if detail := visualString(activity, "detail"); detail != "" {
			text += paint(" "+oneLine(detail), theme.Muted)
		}
		var status string
		switch visualString(activity, "status") {
		case "running":
			status = paint(" (运行中…)", theme.Warning)
		case "failed":
			status = paint(" (失败)", theme.Error)
		}
		if contentWidth := available - ansi.StringWidth(status); contentWidth > 0 {
			text = fit(text, contentWidth) + status
		} else {
			text += status
		}
		rows = append(rows, prefix+fit(text, available))
	}
	return rows
}
