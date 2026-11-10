package render

import (
	"fmt"
	"strings"

	"pi-goui/internal/config"
)

// Worker snapshots retain only a bounded activity history; task usage is not tool usage.
func workerToolRows(card workerCard, theme config.Theme, width int) []string {
	var tools []map[string]any
	for _, value := range card.activities {
		activity := workerMap(value)
		if visualString(activity, "type") != "tool" {
			continue
		}
		tools = append(tools, activity)
	}
	total := max(card.toolCalls, len(tools))
	if total == 0 {
		return nil
	}
	indent := min(2, max(0, width-1))
	prefix := strings.Repeat(" ", indent)
	available := max(1, width-indent)
	header := paint("≡", theme.Muted) + " " + paint(fmt.Sprintf("%d calls", total), theme.Text)
	rows := []string{prefix + fit(header, available)}
	indent = min(4, max(0, width-1))
	prefix = strings.Repeat(" ", indent)
	available = max(1, width-indent)
	shown := min(3, len(tools))
	// No fold control: only the most recent three recorded calls are displayed.
	if hidden := total - shown; hidden > 0 {
		rows = append(rows, prefix+fit(paint(fmt.Sprintf("… %d 次调用 (已折叠)", hidden), theme.Muted), available))
	}
	for _, activity := range tools[len(tools)-shown:] {
		icon, color := workerStatus(visualString(activity, "status"), theme)
		text := paint(icon, color) + " " + paint(oneLine(workerFirst(visualString(activity, "label"), "tool")), theme.Text)
		if detail := visualString(activity, "detail"); detail != "" {
			text += paint(" "+oneLine(detail), theme.Muted)
		}
		switch visualString(activity, "status") {
		case "running":
			text += paint(" (运行中…)", theme.Warning)
		case "failed":
			text += paint(" (失败)", theme.Error)
		}
		rows = append(rows, prefix+fit(text, available))
	}
	return rows
}
