package ui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
)

type sidebarTooltip struct {
	x, y, width, height int
	rows                []string
}

func (m *Model) sidebarHistoryTooltip(t *tab) (sidebarTooltip, bool) {
	if t == nil || m.inspectorOpen() || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" || m.sidebarWidth() == 0 {
		return sidebarTooltip{}, false
	}
	ctx := m.sidebarContext()
	index := -1
	for i, hovered := range ctx.HistoryHovered {
		if hovered {
			index = i
			break
		}
	}
	if index < 0 {
		return sidebarTooltip{}, false
	}
	width := min(40, m.sidebarWidth()-2)
	availableHeight := m.height - 2
	if width < 12 || availableHeight < 5 {
		return sidebarTooltip{}, false
	}
	inner := width - 4
	name := strings.Join(strings.Fields(render.Clean(ctx.History[index])), " ")
	nameRows := strings.Split(ansi.Hardwrap(name, inner, false), "\n")
	stamp := "—"
	if modified := m.sidebarHistoryAt(index).Modified; modified > 0 {
		stamp = time.UnixMilli(modified).Local().Format("2006-01-02 15:04:05")
	}
	stampRows := strings.Split(ansi.Hardwrap(stamp, inner, false), "\n")
	maxNameRows := availableHeight - len(stampRows) - 3 // Frame and blank separator.
	if maxNameRows < 1 {
		return sidebarTooltip{}, false
	}
	if len(nameRows) > maxNameRows {
		nameRows = nameRows[:maxNameRows]
		nameRows[maxNameRows-1] = ansi.Truncate(nameRows[maxNameRows-1], inner-1, "") + "…"
	}
	rows := render.SidebarDetailFrame(nameRows, stampRows, width, m.cfg.Theme)
	layout := m.sidebarLayout()
	itemY := layout.HistoryStart + (index-layout.HistoryOffset)*render.SidebarHistoryRows
	return sidebarTooltip{x: max(0, m.mainWidth()-width+3), y: min(max(1, itemY-1), m.height-1-len(rows)), width: width, height: len(rows), rows: rows}, true
}

func (m *Model) overlaySidebarHistoryTooltip(t *tab, view string) string {
	box, ok := m.sidebarHistoryTooltip(t)
	if !ok {
		return view
	}
	rows := strings.Split(view, "\n")
	for i, line := range box.rows {
		y := box.y + i
		if y >= len(rows) {
			break
		}
		rows[y] = popupBackdropSlice(rows[y], 0, box.x) + line + popupBackdropSlice(rows[y], box.x+box.width, m.width)
	}
	return strings.Join(rows, "\n")
}
