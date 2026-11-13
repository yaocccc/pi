package toolrender

import "pi-goui/internal/core/extension"

type Adapter struct{}

func (Adapter) Capabilities() extension.Capabilities {
	return extension.Capabilities{Tools: NewViews()}
}

var _ extension.Extension = Adapter{}
