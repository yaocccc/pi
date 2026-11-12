package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/session"
)

type imagePreviewMsg struct {
	id  int
	err error
}

// The UI only schedules preview work and routes its result to the owning tab.
type imagePreviewer interface {
	Open(session.Image) error
	Close()
}

func openImagePreview(store imagePreviewer, tabID int, img session.Image) tea.Cmd {
	return func() tea.Msg {
		if store == nil {
			return imagePreviewMsg{id: tabID, err: errors.New("image preview unavailable")}
		}
		return imagePreviewMsg{id: tabID, err: store.Open(img)}
	}
}
