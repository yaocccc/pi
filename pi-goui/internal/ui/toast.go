package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
)

const toastLifetime = 2 * time.Second

type toastState struct {
	text    string
	expires time.Time
}

func (t *tab) notify(text string) { t.notifyAt(text, time.Now()) }
func (t *tab) notifyAt(text string, now time.Time) {
	// Bound retained notice text as well as its eventual display width.
	text = strings.Join(strings.Fields(render.Clean(text)), " ")
	text = ansi.Truncate(text, 256, "…")
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
	if width < 5 || height < 3 {
		return toastBox{}, false
	}
	text := ansi.Truncate(t.toast.text, width-4, "…")
	width = ansi.StringWidth(text) + 4
	color := m.cfg.Theme.Accent
	rows := []string{m.paint("╭"+strings.Repeat("─", width-2)+"╮", color), m.paint("│", color) + " " + m.paint(text, m.cfg.Theme.Text) + " " + m.paint("│", color), m.paint("╰"+strings.Repeat("─", width-2)+"╯", color)}
	return toastBox{x: m.mainWidth() - width - 1, y: transcriptTop, width: width, height: 3, rows: rows}, true
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
		rows[y] = ansi.Cut(rows[y], 0, box.x) + line + ansi.Cut(rows[y], box.x+box.width, m.mainWidth())
	}
	return strings.Join(rows, "\n")
}
