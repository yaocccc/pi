package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

// A prompt echo is a local display projection, never a native history record.
// It survives startup snapshots but is retired by a new authoritative user
// record from the client/cycle to which this particular prompt was dispatched.
type promptEcho struct {
	item       session.Item
	original   string
	images     []map[string]any
	pending    *resumeSubmission
	client     *rpc.Client
	generation uint64
	users      map[string]bool
	dispatched bool
	restored   bool
}

func (m *Model) showPromptEcho(t *tab, original string, images []map[string]any, pending *resumeSubmission) {
	text, attached := referencedImages(strings.TrimSpace(original), images)
	if t.promptEcho != nil || t.readOnly() || t.ownRunActive() || len(t.state.Steering) > 0 || len(t.state.FollowUp) > 0 || strings.HasPrefix(text, "/") || (text == "" && len(attached) == 0) {
		return
	}
	e := &promptEcho{original: original, images: slices.Clone(images), pending: pending,
		item: session.Item{ID: fmt.Sprintf("local-prompt-%d-%d", t.id, t.draftRevision), Role: "user", Text: text}}
	for i := range e.images {
		e.images[i] = maps.Clone(e.images[i])
	}
	for _, image := range attached {
		data, _ := image["data"].(string)
		mime, _ := image["mimeType"].(string)
		e.item.Images = append(e.item.Images, session.Image{Data: data, MimeType: mime})
	}
	t.promptEcho = e
	t.follow = true
	// Reuse the already-published native document. A new bubble must not wait
	// for, or synchronously rebuild, a large history layout already in flight.
	base := t.nativeDocument
	if len(t.state.Items) == 0 {
		base = render.Document{}
	}
	if !t.selection.dragging && !m.panelResize.dragging {
		m.publishDocument(t, base, nil)
	}
}

func (t *tab) dispatchPromptEcho() *promptEcho {
	e := t.promptEcho
	if e == nil || e.dispatched {
		return nil
	}
	e.client, e.generation, e.dispatched = t.client, t.cycle.generation, true
	e.users = map[string]bool{}
	for _, item := range t.state.Items {
		if item.Role == "user" {
			e.users[item.ID] = true
		}
	}
	return e
}

func (t *tab) confirmPromptEcho() bool {
	e := t.promptEcho
	if e == nil || !e.dispatched || e.client == nil || e.client != t.client || e.generation != t.cycle.generation {
		return false
	}
	for _, item := range t.state.Items {
		if item.Role == "user" && !e.users[item.ID] {
			// Use the real item, even if an input extension transformed its text.
			t.promptEcho = nil
			t.invalidateDocument()
			return true
		}
	}
	return false
}

func (m *Model) clearPromptEcho(t *tab, e *promptEcho) {
	if e != nil && t.promptEcho == e {
		t.promptEcho = nil
		if !t.selection.dragging && !m.panelResize.dragging {
			m.publishDocument(t, t.nativeDocument, nil)
		}
	}
}

func (m *Model) restorePromptEcho(t *tab, e *promptEcho) {
	if e == nil || e.restored {
		return
	}
	e.restored = true
	m.clearPromptEcho(t, e)
	m.restoreResumeDraft(t, &resumeSubmission{original: e.original, images: e.images})
}

// Append a normal user bubble to a display-only document, preserving all row
// indices used by selection, scrolling and attachment links. Published slices
// remain immutable, including when an asynchronous native layout arrives.
func (m *Model) echoDocument(t *tab, base render.Document) render.Document {
	if t.promptEcho == nil {
		return base
	}
	var cache render.TranscriptCache
	echo := cache.Transcript([]session.Item{t.promptEcho.item}, m.cfg.Theme, m.documentOptions(t))
	if base.Text == "" {
		return echo
	}
	offset := len(strings.Split(base.Text, "\n")) + 1
	doc := base
	doc.Text = base.Text + "\n\n" + echo.Text
	doc.Blocks = slices.Clone(base.Blocks)
	for _, block := range echo.Blocks {
		block.Start += offset
		block.End += offset
		doc.Blocks = append(doc.Blocks, block)
	}
	doc.ImageLinks = slices.Clone(base.ImageLinks)
	for _, link := range echo.ImageLinks {
		link.Row += offset
		doc.ImageLinks = append(doc.ImageLinks, link)
	}
	doc.Content = append(slices.Clone(base.Content), render.ContentSpan{})
	doc.Content = append(doc.Content, echo.Content...)
	doc.ActivityRows = slices.Clone(base.ActivityRows)
	return doc
}
