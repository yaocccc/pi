package render

import (
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render/tooldetail"
	"pi-goui/internal/core/session"
)

// ToolViews is an immutable, optional set of instance-bound presentation hooks.
// A nil provider preserves native tool rendering and generic fallback.
type ToolViews struct {
	Policy     func(string) extensionapi.CardPolicy
	Layout     func(extensionapi.CardHost, session.Item, config.Theme, extensionapi.CardOptions, int) ([]string, []extensionapi.Block, bool)
	Detail     func(extensionapi.CardHost, session.Item, config.Theme, int) ([]string, bool)
	FullDetail tooldetail.Provider
}

func cardHost() extensionapi.CardHost {
	return extensionapi.CardHost{Map: visualMap, String: visualString, Paint: paint, Clean: Clean, OneLine: oneLine, Fit: fit, Wrap: visualWrap,
		Badge: withToolBadge, Tokens: tokenBadge, Expanded: func(id string, o extensionapi.CardOptions) bool {
			return isExpanded(id, Options{ExpandTools: o.ExpandTools, Expanded: o.Expanded})
		}, Compact: compactBubble, Frame: frame}
}
func cardOptions(o Options) extensionapi.CardOptions {
	return extensionapi.CardOptions{ExpandTools: o.ExpandTools, Expanded: o.Expanded}
}
func firstTools(tools []*ToolViews) *ToolViews {
	if len(tools) > 0 {
		return tools[0]
	}
	return nil
}
func extensionPolicy(name string, tools *ToolViews) extensionapi.CardPolicy {
	if tools != nil && tools.Policy != nil {
		return tools.Policy(name)
	}
	return extensionapi.CardPolicy{}
}
func cardLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block, bool) {
	if opts.Tools != nil && opts.Tools.Layout != nil {
		return opts.Tools.Layout(cardHost(), item, theme, cardOptions(opts), width)
	}
	return nil, nil, false
}
func toolLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	if rows, blocks, ok := cardLayout(item, theme, opts, width); ok {
		return rows, blocks
	}
	rows := toolBody(item, theme, opts, width)
	return rows, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(rows), Foldable: true, Expanded: isExpanded(item.ID, opts)}}
}
