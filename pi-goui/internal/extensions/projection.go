package extensions

import (
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/extensions/sessionview"
)

type projectionAdapter struct{}

func (projectionAdapter) Capabilities() extension.Capabilities {
	return extension.Capabilities{NewProjection: func() extensionapi.SessionProjection { return &sessionview.State{} }}
}

var _ extension.Extension = projectionAdapter{}
