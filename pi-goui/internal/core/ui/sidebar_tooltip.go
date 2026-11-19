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

func (m *Model) canShowSidebarTooltip(t *tab) bool {
	return t != nil && len(t.dialogs) == 0 && !t.selection.dragging && !m.panelResize.dragging && m.searching == "" && m.sidebarWidth() > 0
}

func (m *Model) sidebarHistoryTooltip(t *tab) (sidebarTooltip, bool) {
	if !m.canShowSidebarTooltip(t) {
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
	name := strings.Join(strings.Fields(render.Clean(ctx.History[index])), " ")
	stamp := "—"
	if modified := m.sidebarHistoryAt(index).Modified; modified > 0 {
		stamp = time.UnixMilli(modified).Local().Format("2006-01-02 15:04:05")
	}
	layout := m.sidebarLayout()
	itemY := layout.HistoryStart + (index-layout.HistoryOffset)*render.SidebarHistoryRows
	return m.sidebarDetailTooltip(itemY, name, stamp)
}

func (m *Model) sidebarDetailTooltip(itemY int, title, detail string) (sidebarTooltip, bool) {
	width := min(40, m.sidebarWidth()-2)
	if width < 12 {
		return sidebarTooltip{}, false
	}
	inner := width - 4
	titleRows := strings.Split(ansi.Hardwrap(title, inner, false), "\n")
	detailRows := strings.Split(ansi.Hardwrap(detail, inner, false), "\n")
	maxTitleRows := m.height - 2 - len(detailRows) - 3 // Frame and blank separator.
	if maxTitleRows < 1 {
		return sidebarTooltip{}, false
	}
	if len(titleRows) > maxTitleRows {
		titleRows = titleRows[:maxTitleRows]
		titleRows[maxTitleRows-1] = ansi.Truncate(titleRows[maxTitleRows-1], inner-1, "") + "…"
	}
	rows := render.SidebarDetailFrame(titleRows, detailRows, width, m.cfg.Theme)
	x := max(0, m.sideX()-width+3)
	if m.cfg.SidebarPosition == "left" {
		x = -3
	}
	return sidebarTooltip{x: x, y: min(max(1, itemY-1), m.height-1-len(rows)), width: width, height: len(rows), rows: rows}, true
}

func (m *Model) overlaySidebarHistoryTooltip(t *tab, view string) string {
	box, ok := m.sidebarHistoryTooltip(t)
	if !ok {
		return view
	}
	box.x += m.mainScreenX()
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
