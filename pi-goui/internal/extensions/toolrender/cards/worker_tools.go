package cards

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
func (r *Renderer) WorkerThinkingSteps(text string) []string {
	text = r.Clean(text)
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
func (r *Renderer) WorkerToolRows(card WorkerCard, theme config.Theme, width int) []string {
	type step struct {
		activity map[string]any
		thinking string
	}
	var steps []step
	tools, thoughts := 0, 0
	for _, value := range card.Activities {
		activity := workerMap(value)
		switch r.String(activity, "type") {
		case "tool":
			steps = append(steps, step{activity: activity})
			tools++
		case "thinking":
			for _, text := range r.WorkerThinkingSteps(r.String(activity, "detail")) {
				steps = append(steps, step{thinking: text})
				thoughts++
			}
		}
	}
	total := max(card.ToolCalls, tools) + thoughts
	if total == 0 {
		return nil
	}
	indent := min(2, max(0, width-1))
	prefix := strings.Repeat(" ", indent)
	available := max(1, width-indent)
	header := r.Paint("≡", theme.Muted) + " " + r.Paint("Activity", theme.Text)
	rows := []string{prefix + r.Fit(header, available)}
	indent = min(4, max(0, width-1))
	prefix = strings.Repeat(" ", indent)
	available = min(60, max(1, width-indent))
	shown := min(3, len(steps))
	if hidden := total - shown; hidden > 0 {
		rows = append(rows, prefix+r.Fit(r.Paint(fmt.Sprintf("… %d items (已折叠)", hidden), theme.Muted), available))
	}
	for _, step := range steps[len(steps)-shown:] {
		if step.thinking != "" {
			rows = append(rows, prefix+r.Fit(r.Paint("!", theme.Thinking)+" "+r.Paint(r.OneLine(step.thinking), theme.Muted), available))
			continue
		}
		activity := step.activity
		icon, color := workerStatus(r.String(activity, "status"), theme)
		text := r.Paint(icon, color) + " " + r.Paint(r.OneLine(workerFirst(r.String(activity, "label"), "tool")), theme.Text)
		if detail := r.String(activity, "detail"); detail != "" {
			text += r.Paint(" "+r.OneLine(detail), theme.Muted)
		}
		var status string
		switch r.String(activity, "status") {
		case "running":
			status = r.Paint(" (运行中…)", theme.Warning)
		case "failed":
			status = r.Paint(" (失败)", theme.Error)
		}
		if contentWidth := available - ansi.StringWidth(status); contentWidth > 0 {
			text = r.Fit(text, contentWidth) + status
		} else {
			text += status
		}
		rows = append(rows, prefix+r.Fit(text, available))
	}
	return rows
}
