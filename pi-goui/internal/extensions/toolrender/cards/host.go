// Package cards owns third-party compact tool layouts, never execution state.
package cards

import "pi-goui/internal/extensionapi"

type Renderer struct{ extensionapi.CardHost }

func New(host extensionapi.CardHost) *Renderer { return &Renderer{host} }

type Options = extensionapi.CardOptions
type Block = extensionapi.Block
