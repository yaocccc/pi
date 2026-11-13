package toolrender

import (
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender/cards"
	"pi-goui/internal/extensions/toolrender/detail"
)

// NewViews assembles a fresh immutable provider. Mutable output belongs to each
// rendering call; host code chooses explicitly whether to install these views.
func NewViews() *render.ToolViews {
	return &render.ToolViews{
		Policy: cards.Policy,
		Layout: func(host extensionapi.CardHost, item session.Item, theme config.Theme, opts extensionapi.CardOptions, width int) ([]string, []extensionapi.Block, bool) {
			return cards.New(host).Layout(item, theme, opts, width)
		},
		Detail: func(host extensionapi.CardHost, item session.Item, theme config.Theme, width int) ([]string, bool) {
			return cards.New(host).Detail(item, theme, width)
		},
		FullDetail: func(host extensionapi.DetailHost, name string, item session.Item, args, detailsMap map[string]any) bool {
			return detail.New(host).Render(name, item, args, detailsMap)
		},
	}
}
