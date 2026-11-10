package render

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// These foreground roles and thresholds mirror extensions/ui/folding.ts and
// tool-tokens.ts. Estimates describe saved payload size, never provider billing.
func tokenBadge(tokens int, theme config.Theme) string {
	if tokens <= 0 {
		return ""
	}
	count := fmt.Sprint(tokens)
	if tokens >= 1_000_000 {
		count = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	} else if tokens >= 1_000 {
		count = fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	color := visualDim(theme)
	if tokens >= 10_000 {
		color = theme.Error
	} else if tokens >= 5_000 {
		color = "#ffa500"
	}
	return paint("≈"+count+" tok", color)
}
func withToolBadge(content, badge string, width, minWidth int, right bool) string {
	if badge == "" || width < minWidth {
		return fit(content, width)
	}
	available := width - ansi.StringWidth(badge) - 1
	if available < 1 {
		return fit(content, width)
	}
	content = fit(content, available)
	padding := 1
	if right {
		padding = max(1, width-ansi.StringWidth(content)-ansi.StringWidth(badge))
	}
	return content + strings.Repeat(" ", padding) + badge
}
func toolCallHeader(item session.Item, expanded bool, theme config.Theme, width int) string {
	// Keep compact tool summaries from stretching the assistant bubble.
	width = min(width, 80)
	arrow := "›"
	if expanded {
		arrow = "⌄"
	}
	symbol, color := "✓", theme.Success
	if !item.HasResult && item.Text == "" {
		symbol, color = "○", theme.Muted
	}
	if item.Running {
		symbol, color = "⋯", theme.Warning
	}
	if item.Error {
		symbol, color = "✗", theme.Error
	}
	name := oneLine(item.ToolName)
	if name == "" {
		name = "tool"
	}
	header := paint(arrow, theme.Muted) + " " + paint(symbol, color) + " " + paint(name, theme.Text)
	if preview := toolPreview(item); preview != "" {
		header += "  " + paint(oneLine(preview), visualDim(theme))
	}
	return withToolBadge(header, tokenBadge(item.EstimatedTokens, theme), width, 80, false)
}
func toolGroupHeader(items []session.Item, expanded bool, theme config.Theme, width int, active ...bool) string {
	arrow := "›"
	if expanded {
		arrow = "⌄"
	}
	counts := map[string]int{}
	var names []string
	failed, tokens := 0, 0
	for _, item := range items {
		name := oneLine(item.ToolName)
		if name == "" {
			name = "tool"
		}
		if counts[name] == 0 {
			names = append(names, name)
		}
		counts[name]++
		if item.Error {
			failed++

		}
		tokens += item.EstimatedTokens
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%d%s", counts[name], name))
	}
	arrowColor := theme.Muted
	if len(active) > 0 && active[0] {
		arrow, arrowColor = "⋯", theme.Warning
	}
	header := paint(arrow, arrowColor) + " "
	header += paint(fmt.Sprintf("%d calls", len(items)), theme.Text)
	header += paint(" · ", theme.Muted) + paint(strings.Join(parts, "  "), visualDim(theme))
	status := ""
	if failed > 0 {
		status = paint(fmt.Sprintf(" %d❌", failed), theme.Error)
	}
	badge := strings.Replace(tokenBadge(tokens, theme), " tok", "", 1)
	available := width
	if badge != "" && width >= 24 {
		available -= ansi.StringWidth(badge) + 1
	}
	if available > ansi.StringWidth(status) {
		header = fit(header, available-ansi.StringWidth(status)) + status
	} else {
		header = fit(header, available)
	}
	return withToolBadge(header, badge, width, 24, false)
}
