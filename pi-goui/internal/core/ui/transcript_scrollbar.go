package ui

import (
	"pi-goui/internal/core/render"
	"strings"
	"time"
)

func (t *tab) noteScroll() { t.scrollbarUntil = time.Now().Add(800 * time.Millisecond) }

func transcriptThumb(total, height, offset int) (start, size int) {
	if height <= 0 || total <= height {
		return 0, 0
	}
	size = max(1, height*height/total)
	start = min(max(0, offset), total-height) * (height - size) / (total - height)
	return start, size
}

func (m *Model) transcriptViewport(t *tab, text string, total, height int) string {
	width := m.mainWidth()
	body := fitBox(text, width, height)
	start, size := transcriptThumb(total, height, t.offset)
	if size == 0 || !time.Now().Before(t.scrollbarUntil) {
		return body
	}
	rows := strings.Split(body, "\n")
	for i := range rows {
		cell := render.ScrollbarMark("│")
		if i >= start && i < start+size {
			cell = render.ScrollbarMark("┃")
		}
		// Overlay a terminal cell; never split a wide grapheme at the edge.
		rows[i] = popupBackdropSlice(rows[i], 0, width-1) + cell
	}
	return strings.Join(rows, "\n")
}
