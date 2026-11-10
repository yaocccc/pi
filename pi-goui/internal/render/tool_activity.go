package render

import (
	"strings"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// Only the latest tool run can have an activity card. Assistant prose, a new
// user message or a system boundary ends it; thinking alone does not.
func toolActivityGroup(items []session.Item, opts Options) string {
	if !opts.ShowToolActivity {
		return ""
	}
	visible := visibleItems(items, opts)
	end := len(visible) - 1
	for end >= 0 {
		item := visible[end]
		if item.Role == "thinking" || (item.Role == "assistant" && strings.TrimSpace(item.Text) == "" && !item.Error) {
			end--
			continue
		}
		if item.Role != "tool" && item.Role != "toolResult" {
			return ""
		}
		start := end
		for start > 0 && (visible[start-1].Role == "tool" || visible[start-1].Role == "toolResult") {
			start--
		}
		return "tool-group:" + visible[start].ID
	}
	return ""
}

func toolActivityLayout(items []session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	workerCard := func(item session.Item) bool {
		return visualName(item.ToolName) == "worker"
	}
	start := max(0, len(items)-3)
	limit := 80
	for _, item := range items[start:] {
		if workerCard(item) {
			limit = width
			break
		}
	}
	width = max(1, min(width, limit))
	indent := min(2, max(0, width-1))
	inner := max(1, width-indent)
	var rows []string
	var blocks []Block
	if start > 0 {
		rows = append(rows, paint("…", theme.Muted))
	}
	for _, item := range items[start:] {
		// Share ordinary tool rendering and fold IDs with the full list.
		// Only recent Workers default open; explicit per-tool folds always win.
		childOpts := opts
		childOpts.ExpandTools = workerCard(item)
		childRows, childBlocks := toolLayout(item, theme, childOpts, inner)
		for _, block := range childBlocks {
			block.Start += len(rows)
			block.End += len(rows)
			blocks = append(blocks, block)
		}
		rows = append(rows, childRows...)
	}
	var result []string
	for _, row := range rows {
		result = append(result, strings.Repeat(" ", indent)+row)
	}
	return result, blocks
}
