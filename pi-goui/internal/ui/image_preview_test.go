package ui

import (
	"errors"
	"testing"

	"pi-goui/internal/session"
)

type previewStub struct {
	open   func(session.Image) error
	closed bool
}

func (s *previewStub) Open(img session.Image) error { return s.open(img) }
func (s *previewStub) Close()                       { s.closed = true }

func TestImagePreviewCommandRoutesResult(t *testing.T) {
	img := session.Image{Data: "image", MimeType: "image/png"}
	wantErr := errors.New("fixture launch failure")
	calls := 0
	store := &previewStub{open: func(got session.Image) error {
		calls++
		if got != img {
			t.Fatal("preview image changed")
		}
		return wantErr
	}}
	cmd := openImagePreview(store, 42, img)
	if calls != 0 {
		t.Fatal("viewer launched before command ran")
	}
	msg := cmd().(imagePreviewMsg)
	if msg.id != 42 || msg.err != wantErr || calls != 1 {
		t.Fatalf("incorrect result: %+v", msg)
	}
	msg = openImagePreview(nil, 44, img)().(imagePreviewMsg)
	if msg.id != 44 || msg.err == nil {
		t.Fatal("nil store accepted or session lost")
	}
}

func TestModelClosesImagePreviewStore(t *testing.T) {
	m := testModel(t, "")
	store := &previewStub{}
	m.imagePreviews = store
	m.Close()
	if !store.closed {
		t.Fatal("model did not close its preview store")
	}
}
