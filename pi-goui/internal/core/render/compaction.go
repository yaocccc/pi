package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func compactionStats(m *session.CompactionMetrics) string {
	if m == nil || m.BeforeTokens < 0 || m.AfterTokens < 0 {
		return ""
	}
	text := fmt.Sprintf("≈%s → %s tokens", number(m.BeforeTokens), number(m.AfterTokens))
	if m.BeforeTokens > 0 {
		change := (float64(m.AfterTokens)/float64(m.BeforeTokens) - 1) * 100
		percent := fmt.Sprintf("%+.0f%%", change)
		if change == 0 {
			percent = "0%"
		}
		text += " · " + strings.ReplaceAll(percent, "-", "−")
	}
	return text
}

func systemRows(item session.Item, width int, theme config.Theme) []string {
	rows := []string{paint(fit(systemSynopsis(item.Text), width), theme.Muted)}
	if stats := compactionStats(item.Compaction); stats != "" {
		// Unlike the synopsis, never clip away the metrics on narrow terminals.
		for _, line := range strings.Split(ansi.Wrap(stats, width, ""), "\n") {
			rows = append(rows, paint(line, theme.Text))
		}
	}
	return rows
}
