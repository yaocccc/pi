package render

import (
	"pi-goui/internal/config"
	"pi-goui/internal/extensionapi"
	"pi-goui/internal/extensions/toolrender/cards"
	"pi-goui/internal/session"
)

// The host contributes generic drawing primitives, not extension policies.
func extensionCards() *cards.Renderer {
	return cards.New(extensionapi.CardHost{Map: visualMap, String: visualString, Paint: paint, Clean: Clean, OneLine: oneLine, Fit: fit, Wrap: visualWrap,
		Badge: withToolBadge, Tokens: tokenBadge, Expanded: func(id string, o extensionapi.CardOptions) bool {
			return isExpanded(id, Options{ExpandTools: o.ExpandTools, Expanded: o.Expanded})
		}, Compact: compactBubble, Frame: frame})
}
func cardOptions(o Options) extensionapi.CardOptions {
	return extensionapi.CardOptions{ExpandTools: o.ExpandTools, Expanded: o.Expanded}
}
func workerLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	return extensionCards().WorkerLayout(item, theme, cardOptions(opts), width)
}
func workerDetailRows(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	return extensionCards().WorkerDetailRows(item, theme, cardOptions(opts), width)
}
func questionRows(item session.Item, theme config.Theme, width int) []string {
	return extensionCards().QuestionRows(item, theme, width)
}

func toolLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	if visualName(item.ToolName) == "worker" {
		return workerLayout(item, theme, opts, width)
	}
	rows := toolBody(item, theme, opts, width)
	return rows, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(rows), Foldable: true, Expanded: isExpanded(item.ID, opts)}}
}
