package hostui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

type ConversationTarget = extension.ConversationTarget

// Build hit regions once per document, not once per pointer-motion event. The
// coordinates are document cells; the viewport only translates and clips them.
func ConversationTargets(doc render.Document, items []session.Item, toolInspectButton string) []ConversationTarget {
	tools := map[string]bool{}
	workers := map[string]bool{}
	for _, it := range items {
		if it.Role == "tool" || it.Role == "toolResult" {
			tools[it.ID] = true
			workers[it.ID] = it.ToolName == "worker" || strings.HasSuffix(it.ToolName, ".worker")
		}
	}
	var out []ConversationTarget
	for _, b := range doc.Blocks {
		top := b.Start + 1
		if !tools[b.ID] || !b.Expanded || top >= doc.LineCount() {
			continue
		}
		s := ansi.Strip(doc.Line(top))
		l, r := strings.IndexAny(s, "╭┌"), strings.LastIndexAny(s, "╮┐")
		if l < 0 || r < l {
			continue
		}
		left, right := ansi.StringWidth(s[:l]), ansi.StringWidth(s[:r])
		width := ansi.StringWidth(toolInspectButton)
		if right-left-1 < width {
			continue
		}
		if !workers[b.ID] && top+1 < b.End {
			out = append(out, ConversationTarget{ID: b.ID, Rect: Rect{X: right - width, Y: top + 1, Width: width}})
		}
		if workers[b.ID] {
			for _, child := range doc.Blocks {
				if child.InspectorParent == b.ID && child.InspectorTask != "" {
					out = append(out, ConversationTarget{ID: child.ID, Rect: Rect{X: right - width, Y: child.Start, Width: width}})
				}
			}
		}
	}
	return out
}
