package extension

import (
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func (r *Runtime) toolViews() *render.ToolViews {
	var providers []*render.ToolViews
	for _, c := range r.capabilities {
		if c.Tools != nil {
			providers = append(providers, c.Tools)
		}
	}
	if len(providers) == 0 {
		return nil
	}
	// Allocate even for a single provider: cache identity belongs to this runtime.
	return &render.ToolViews{
		Policy: func(name string) extensionapi.CardPolicy {
			for _, p := range providers {
				if p.Policy != nil {
					if policy := p.Policy(name); policy != (extensionapi.CardPolicy{}) {
						return policy
					}
				}
			}
			return extensionapi.CardPolicy{}
		},
		Layout: func(h extensionapi.CardHost, item session.Item, t config.Theme, o extensionapi.CardOptions, w int) ([]string, []extensionapi.Block, bool) {
			for _, p := range providers {
				if p.Layout != nil {
					if rows, blocks, ok := p.Layout(h, item, t, o, w); ok {
						return rows, blocks, true
					}
				}
			}
			return nil, nil, false
		},
		Detail: func(h extensionapi.CardHost, item session.Item, t config.Theme, w int) ([]string, bool) {
			for _, p := range providers {
				if p.Detail != nil {
					if rows, ok := p.Detail(h, item, t, w); ok {
						return rows, true
					}
				}
			}
			return nil, false
		},
		FullDetail: func(h extensionapi.DetailHost, name string, item session.Item, args, details map[string]any) bool {
			for _, p := range providers {
				if p.FullDetail != nil && p.FullDetail(h, name, item, args, details) {
					return true
				}
			}
			return false
		},
	}
}
