package ui

import (
	"fmt"
	"regexp"
	"strconv"

	tea "charm.land/bubbletea/v2"
)

var imageReference = regexp.MustCompile(`\[image([1-9][0-9]*)\]`)

type imagePasteAnchor struct {
	text   string
	offset int
}

// Keep the original insertion point when typing continues during clipboard IO.
func (a imagePasteAnchor) position(current string) int {
	old, next := []rune(a.text), []rune(current)
	prefix := 0
	for prefix < len(old) && prefix < len(next) && old[prefix] == next[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(old)-prefix && suffix < len(next)-prefix && old[len(old)-1-suffix] == next[len(next)-1-suffix] {
		suffix++
	}
	if a.offset <= prefix {
		return a.offset
	}
	if a.offset >= len(old)-suffix {
		return max(0, min(len(next), a.offset+len(next)-len(old)))
	}
	return prefix
}

func (m *Model) startImagePaste(t *tab, cmd tea.Cmd, atStart bool) tea.Cmd {
	anchor := imagePasteAnchor{t.editor.Value(), t.editor.CursorOffset()}
	if atStart {
		anchor = imagePasteAnchor{}
	}
	generation := t.imageGeneration
	t.imagePending++
	return func() tea.Msg {
		msg := cmd().(clipboardImageMsg)
		msg.anchor, msg.generation, msg.pending = &anchor, generation, true
		return msg
	}
}
func (t *tab) clearImages() {
	t.images = nil
	t.imagePending = 0
	t.imageGeneration++
}
func (m *Model) attachImage(t *tab, image map[string]any, anchor *imagePasteAnchor) {
	t.images = append(t.images, image)
	marker := fmt.Sprintf("[image%d]", len(t.images))
	position := t.editor.CursorOffset()
	if anchor != nil {
		position = anchor.position(t.editor.Value())
	}
	cursor := t.editor.CursorOffset()
	t.editor.CompleteRange(position, position, marker, len([]rune(marker)))
	// Do not pull the caret backwards if the user continued typing during IO.
	if anchor != nil && cursor != position {
		if cursor > position {
			cursor += len([]rune(marker))
		}
		t.editor.CompleteRange(cursor, cursor, "", 0)
	}
	t.dismissCompletedWorking()
	t.draftRevision++
	t.historyIndex = -1
	m.completionHidden = false
	m.completionIndex = 0
	m.resizeEditor(t)
	t.notify("Image attached")
}

// The RPC accepts text plus an ordered image array. Keep references in the text,
// order attachments by their first reference, and renumber both consistently.
// Unreferenced images stay in the draft for undo, but are never transmitted.
func referencedImages(text string, images []map[string]any) (string, []map[string]any) {
	var selected []map[string]any
	numbers := make(map[int]int)
	text = imageReference.ReplaceAllStringFunc(text, func(marker string) string {
		index, err := strconv.Atoi(marker[6 : len(marker)-1])
		if err != nil || index < 1 || index > len(images) {
			return marker
		}
		number, ok := numbers[index]
		if !ok {
			selected = append(selected, images[index-1])
			number = len(selected)
			numbers[index] = number
		}
		return fmt.Sprintf("[image%d]", number)
	})
	return text, selected
}
