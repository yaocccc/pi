package ui

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
)

const toastLifetime = 5 * time.Second

type toastState struct {
	text    string
	expires time.Time
}

func (t *tab) notify(text string) { t.notifyAt(text, time.Now()) }
func (t *tab) notifyAt(text string, now time.Time) {
	// Preserve paragraphs while removing terminal commands and normalizing spaces.
	lines := strings.Split(render.Clean(text), "\n")
	for i := range lines {
		lines[i] = strings.Join(strings.Fields(lines[i]), " ")
	}
	text = strings.TrimSpace(strings.Join(lines, "\n"))
	// Bound retained text without imposing the old single-line display limit.
	const maxToastBytes = 64 * 1024
	if len(text) > maxToastBytes {
		end := maxToastBytes
		for !utf8.RuneStart(text[end]) {
			end--
		}
		text = text[:end] + "…"
	}
	t.toast = toastState{text: text, expires: now.Add(toastLifetime)}
}

type toastBox struct {
	x, y, width, height int
	rows                []string
}

func (b toastBox) contains(x, y int) bool {
	return x >= b.x && x < b.x+b.width && y >= b.y && y < b.y+b.height
}
func (m *Model) toastBox(t *tab, now time.Time) (toastBox, bool) {
	if t.toast.text == "" || !now.Before(t.toast.expires) || m.height < 8 || len(t.dialogs) > 0 {
		return toastBox{}, false
	}
	width := min(42, m.mainWidth()-2)
	// Use only actual transcript rows, never the working row/footer when space is tight.
	height := min(m.transcriptHeight(t), m.editorTop(t)-1)
	if width < 6 || height < 3 {
		return toastBox{}, false
	}
	inner := width - 4
	lines := strings.Split(ansi.Wrap(t.toast.text, inner, ""), "\n")
	if len(lines) > height-2 {
		lines = lines[:height-2]
		last := len(lines) - 1
		lines[last] = ansi.Truncate(lines[last]+"…", inner, "…")
	}
	contentWidth := 0
	for _, line := range lines {
		contentWidth = max(contentWidth, ansi.StringWidth(line))
	}
	width = contentWidth + 4
	color := m.cfg.Theme.Accent
	rows := []string{m.paint("╭"+strings.Repeat("─", width-2)+"╮", color)}
	for _, line := range lines {
		rows = append(rows, m.paint("│", color)+" "+m.paint(fitLine(line, contentWidth), color)+" "+m.paint("│", color))
	}
	rows = append(rows, m.paint("╰"+strings.Repeat("─", width-2)+"╯", color))
	return toastBox{x: m.mainWidth() - width - 1, y: transcriptTop, width: width, height: len(rows), rows: rows}, true
}
func (m *Model) overlayToast(t *tab, view string, now time.Time) string {
	box, ok := m.toastBox(t, now)
	if !ok {
		return view
	}
	rows := strings.Split(view, "\n")
	for i, line := range box.rows {
		y := box.y + i
		if y >= len(rows) {
			break
		}
		rows[y] = popupBackdropSlice(rows[y], 0, box.x) + line + popupBackdropSlice(rows[y], box.x+box.width, m.mainWidth())
	}
	return strings.Join(rows, "\n")
}
