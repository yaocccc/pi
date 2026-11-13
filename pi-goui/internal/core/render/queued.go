package render

import (
	"strings"

	"pi-goui/internal/core/config"
)

// Queued renders pending messages in one bounded, right-aligned user bubble.
func Queued(messages []string, hint string, width, maxRows int, theme config.Theme) []string {
	if len(messages) == 0 {
		return nil
	}
	width = max(1, width)
	bubbleWidth := width
	if width >= 20 {
		bubbleWidth = width * 4 / 5
	}
	contentWidth := max(1, bubbleWidth-4)
	budget := max(1, maxRows-3) // frame and editing hint
	body := markdown(strings.Join(messages, "\n\n"), contentWidth, theme)
	if len(body) > budget {
		body = append(body[:budget-1], paint(fit("…", contentWidth), theme.Muted))
	}
	body = append(body, paint(fit(hint+" · 编辑", contentWidth), theme.Muted))
	if width >= 8 {
		body, bubbleWidth = compactBubble(body, "你 queued", bubbleWidth)
		body, _ = frameContent(body, "你 queued", bubbleWidth, "user", true, width, theme)
	}
	return body
}
