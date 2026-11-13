// Package extensions supplies the fixed, optional frontend implementation list.
// Core consumes only this slice; each entry implements its neutral contract.
package extensions

import (
	"pi-goui/internal/core/extension"
	"pi-goui/internal/extensions/hostui"
	"pi-goui/internal/extensions/toolrender"
)

// New returns a fresh list, not a mutable global registry. Runtime construction
// creates renderer instances; factories keep tab and working-run state separate.
func New() []extension.Extension {
	return []extension.Extension{hostui.Adapter{}, projectionAdapter{}, toolrender.Adapter{}}
}
