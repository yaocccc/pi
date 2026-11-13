package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

// SidebarSessionRows is the number of terminal rows occupied by a session card.
const SidebarSessionRows = 2

// SidebarSessionCapacity reports how many complete cards fit in the sidebar.
func SidebarSessionCapacity(height int, contexts ...SidebarContext) int {
	var context SidebarContext
	if len(contexts) > 0 {
		context = contexts[0]
	}
	return SidebarGeometry(height, context).Capacity
}

const (
	workbenchText   = "200;200;204"
	workbenchMuted  = "153;158;163"
	workbenchAccent = "183;151;229"
)

// workbenchStyle resets only the attributes it changes; never paint a background.
func workbenchStyle(text, color string, bold bool) string {
	if text == "" {
		return ""
	}
	start, end := "", ""
	if bold {
		start, end = "\x1b[1m", "\x1b[22m"
	}
	return start + "\x1b[38;2;" + color + "m" + text + "\x1b[39m" + end
}

// ScrollbarMark shares the muted palette between RECENTS and the transcript.
func ScrollbarMark(mark string) string { return workbenchStyle(mark, workbenchMuted, false) }

// SessionSectionTitle and NewSessionLabel use the workbench palette.
func SessionSectionTitle() string { return workbenchStyle("SESSIONS", workbenchText, true) }
func NewSessionLabel() string     { return workbenchStyle("+ New session", workbenchAccent, false) }

// SidebarNewButton makes the entire inner row clickable, including padding.
func SidebarNewButton(width int) (start, length int) {
	return 0, max(0, width)
}

// SidebarSessionCloseButton shares the cell geometry with mouse hit testing.
// Hide the button if there is not room for both an ordinal and the full label.
func SidebarSessionCloseButton(width int) (start, length int) {
	if width < 8 {
		return 0, 0
	}
	return width - 4, 4 // 关闭 occupies four terminal cells.
}

// sidebarHistoryRow reserves a navigation gutter before truncating the name.
func sidebarHistoryRow(name string, width int, active bool, depths ...int) string {
	marker := fit("  ", width)
	color, markerColor := workbenchText, workbenchMuted
	if active {
		color, markerColor = workbenchAccent, workbenchAccent
	}
	prefix := ""
	if len(depths) > 0 {
		prefix = session.ThreadPrefix(depths[0])
	}
	name = fit(prefix+oneLine(name), max(0, width-ansi.StringWidth(marker)))
	return workbenchStyle(marker, markerColor, false) + workbenchStyle(name, color, active)
}

func workbenchStatus(status string) (string, string) {
	status = oneLine(status)
	state, suffix, _ := strings.Cut(status, " ")
	label, color := state, workbenchMuted
	switch state {
	case "idle":
		color = "116;192;133"
	case "working", "running":
		color = workbenchAccent
	case "blocked", "input":
		label, color = "等待回答", "221;190;112"
	case "done":
		color = "113;181;166"
	case "error":
		color = "218;126;126"
	case "starting", "closing", "stopped":
		color = workbenchMuted
	}
	if suffix != "" {
		label += " " + suffix
	}
	return label, color
}

// sidebarSessionCards returns only complete two-row cards. The caller must map
// both rows to the same session index; height is the space below the heading.
func sidebarSessionCards(sessions []SessionRow, width, height int) []string {
	if width <= 0 || height < SidebarSessionRows {
		return nil
	}
	count := min(len(sessions), height/SidebarSessionRows)
	rows := make([]string, 0, count*SidebarSessionRows)
	for _, s := range sessions[:count] {
		marker := " "
		if s.Active {
			marker = "›"
		}
		marker = fit(marker, width)
		remaining := max(0, width-ansi.StringWidth(marker))
		id := oneLine(s.ID)
		status, statusColor := workbenchStatus(s.Status)
		if remaining > 0 {
			marker += " "
			remaining--
		}
		// Preserve the ordinal and spend the remaining width on status/unread.
		id = fit(id, remaining)
		remaining -= ansi.StringWidth(id)
		row := workbenchStyle(marker, workbenchAccent, false) + workbenchStyle(id, workbenchText, s.Active)
		if status != "" && remaining > 0 {
			row += " "
			remaining--
			row += workbenchStyle(fit(status, remaining), statusColor, false)
		}
		if start, length := SidebarSessionCloseButton(width); s.Hovered && length > 0 {
			row = ansi.Truncate(row, start-1, "")
			row += strings.Repeat(" ", max(0, start-ansi.StringWidth(row)))
			row += workbenchStyle("关闭", "190;190;190", false)
		}
		name := oneLine(s.Name)
		indent := strings.Repeat(" ", min(2, width))
		name = fit(name, max(0, width-ansi.StringWidth(indent)))
		nameRow := indent + workbenchStyle(name, workbenchText, s.Active)
		if s.Active || s.Hovered {
			background := "57;64;66"
			if s.Active {
				background = "82;85;88"
			}
			highlight := func(line string) string {
				return "\x1b[48;2;" + background + "m" + line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line))) + "\x1b[49m"
			}
			row, nameRow = highlight(row), highlight(nameRow)
		}
		rows = append(rows, row, nameRow)
	}
	return rows
}
