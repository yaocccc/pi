package cards

import (
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render/tooldata"
	"pi-goui/internal/core/session"
)

// Policy owns extension-specific presentation decisions. Unknown tools retain
// the host's generic fallback; native tools and codemode are not claimed here.
func Policy(name string) extensionapi.CardPolicy {
	switch tooldata.ToolName(name) {
	case "worker":
		return extensionapi.CardPolicy{Specialized: true, DefaultExpanded: true}
	case "ask_question":
		return extensionapi.CardPolicy{Specialized: true, Compact: true}
	default:
		return extensionapi.CardPolicy{}
	}
}

// Layout optionally supplies a complete card, including its child hit regions.
func (r *Renderer) Layout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block, bool) {
	if tooldata.ToolName(item.ToolName) != "worker" {
		return nil, nil, false
	}
	rows, blocks := r.WorkerLayout(item, theme, opts, width)
	return rows, blocks, true
}

// Detail supplies compact transcript content, not the full inspector document.
func (r *Renderer) Detail(item session.Item, theme config.Theme, width int) ([]string, bool) {
	switch tooldata.ToolName(item.ToolName) {
	case "worker":
		rows, _ := r.WorkerDetailRows(item, theme, Options{ExpandTools: true}, width)
		return rows, true
	case "ask_question":
		return r.QuestionRows(item, theme, width), true
	default:
		return nil, false
	}
}
