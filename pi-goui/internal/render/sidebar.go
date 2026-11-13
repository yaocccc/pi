package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	frames "pi-goui/internal/render/frame"
	"pi-goui/internal/session"
)

// Default positions with empty history and no quota. Use SidebarGeometry for hit tests.
const SidebarNewRow = 13
const SidebarSessionStart = 15

// Each history entry occupies one row.
const SidebarHistoryRows = 1

type SessionRow struct {
	ID, Name, Status string
	Active, Hovered  bool
}
type GitStatus struct {
	Branch                                 string
	Staged, Modified, Untracked, Conflicts int
	Truncated                              bool
}

// SidebarQuota contains presentation data only. The caller gates OAuth eligibility
// and supplies Now; rendering never reads the clock, credentials or network.
type SidebarQuota struct {
	RemainingPercent *float64
	ResetAt          time.Time
	Credits          *float64
	UnlimitedCredits bool
	Now              time.Time
}
type SidebarContext struct {
	Directory      string
	Git            *GitStatus
	Mode           string
	Quota          *SidebarQuota
	History        []string
	HistoryHovered []bool
	HistoryOffset  int
	HistoryDepth   []int
	Todo           []session.PlanStep
	TodoScroll     int // Rows back from the latest steps; zero follows the end.
	TodoFrame      int // Shared working-message animation frame.
}
type SidebarLayout struct {
	DirectoryRow, ContextRow, StatsRow, QuotaRow, NewRow, SessionsStart, Capacity int
	HistoryRow, HistoryStart, HistoryCount, HistoryOffset                         int
	ModeRow                                                                       int
	TodoRow, TodoStart, TodoCount, TodoOffset                                     int
}

func SidebarGeometry(height int, context SidebarContext) SidebarLayout {
	extra := 0
	quotaRow := -1
	modeRow := -1
	if context.Quota != nil {
		quotaRow = 10
		extra += 4
	}
	if context.Mode != "" {
		modeRow = 10 + extra
		if context.Quota != nil {
			modeRow-- // Insert directly after Credits, before the separator.
		}
		extra++
	}
	todoRow, todoCount := -1, min(10, len(context.Todo), max(0, height-(10+extra)-11-min(1, len(context.History))))
	todoHeight := 0
	if todoCount > 0 {
		todoHeight = todoCount + 1 // Separator followed immediately by steps.
		todoRow = height - 1 - todoHeight
	}
	historyRow := 10 + extra
	// Keep only complete history entries and room for New session and one open-session card.
	historyCount := min(5, len(context.History), max(0, (height-historyRow-todoHeight-8)/SidebarHistoryRows))
	extra += historyCount * SidebarHistoryRows
	newRow := SidebarNewRow + extra
	start := SidebarSessionStart + extra
	directoryRow := -1
	if height > 4 {
		directoryRow = 3
	}
	return SidebarLayout{
		DirectoryRow: directoryRow, ContextRow: 4, StatsRow: 6, QuotaRow: quotaRow, ModeRow: modeRow,
		TodoRow: todoRow, TodoStart: todoRow + 1, TodoCount: todoCount,
		TodoOffset: max(0, len(context.Todo)-todoCount-max(0, context.TodoScroll)),
		HistoryRow: historyRow, HistoryStart: historyRow + 2, HistoryCount: historyCount,
		HistoryOffset: min(max(0, context.HistoryOffset), max(0, len(context.History)-historyCount)),
		NewRow:        newRow, SessionsStart: start,
		Capacity: max(0, (height-start-todoHeight-1)/SidebarSessionRows),
	}
}
func contextValues(state session.State) (percent float64, used, total string) {
	st := state.Stats
	window := st.ContextWindow
	if window <= 0 {
		window = state.Model.ContextWindow
	}
	percent = -1
	if st.ContextPercent != nil {
		percent = *st.ContextPercent
	} else if st.ContextTokens != nil && *st.ContextTokens >= 0 && window > 0 {
		percent = float64(*st.ContextTokens) * 100 / float64(window)
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 {
		percent = -1
	}
	used, total = "—", "—"
	if st.ContextTokens != nil && *st.ContextTokens >= 0 {
		used = number(*st.ContextTokens)
	}
	if window > 0 {
		total = number(window)
	}
	return
}
func progressRow(state session.State, theme config.Theme, width int) string {
	percent, used, total := contextValues(state)
	pct := "—%"
	if percent >= 0 {
		pct = fmt.Sprintf("%.1f%%", percent)
	}
	suffix := pct + " " + used + "/" + total
	cells := width - 4 - 1 - ansi.StringWidth(suffix)
	if cells < 1 {
		return fit(paint("CTX "+suffix, theme.Muted), width)
	}
	filled := 0
	if percent >= 0 {
		filled = int(math.Round(float64(cells) * min(100.0, percent) / 100))
	}
	color := theme.Accent
	if percent >= 90 {
		color = theme.Error
	} else if percent >= 75 {
		color = theme.Warning
	}
	dim := theme.Dim
	if dim == "" {
		dim = theme.Muted
	}
	return paint("CTX ", theme.Muted) + paint(strings.Repeat("━", filled), color) + paint(strings.Repeat("━", cells-filled), dim) + " " + paint(suffix, theme.Text)
}

func gitRow(g *GitStatus, theme config.Theme, width int) string {
	changes := []string{}
	if g.Modified > 0 {
		changes = append(changes, fmt.Sprintf("M%d", g.Modified))
	}
	if g.Staged > 0 {
		changes = append(changes, fmt.Sprintf("+%d", g.Staged))
	}
	if g.Untracked > 0 {
		changes = append(changes, fmt.Sprintf("?%d", g.Untracked))
	}
	if g.Conflicts > 0 {
		changes = append(changes, fmt.Sprintf("!%d", g.Conflicts))
	}
	if g.Truncated {
		changes = append(changes, "…")
	}
	status, color := "clean", theme.Success
	if len(changes) > 0 {
		status, color = strings.Join(changes, " "), theme.Warning
	}
	if g.Conflicts > 0 {
		color = theme.Error
	}
	branch := oneLine(g.Branch)
	if branch == "" {
		branch = "detached"
	}
	suffix := " · " + status
	return fit(paint(fit(branch, max(1, width-ansi.StringWidth(suffix))), theme.Muted)+paint(suffix, color), width)
}

// CodexModeLabel is presentation-only; persisted mode values remain unchanged.
func CodexModeLabel(mode string) string {
	switch mode {
	case "fast":
		return "fast x2.5"
	case "ultrafast":
		return "ultrafast x8"
	default:
		return "normal x1"
	}
}

// SidebarModeValue shares the displayed value and its offset within the inner
// sidebar row with pointer hit testing. The label and separating space are not
// part of the target, even when the value needs truncation.
func SidebarModeValue(mode string, inner int) (value string, offset int) {
	if inner <= 5 {
		return "", max(0, inner)
	}
	value = ansi.Truncate(CodexModeLabel(mode), inner-5, "…")
	return value, inner - ansi.StringWidth(value)
}

func sidebarPair(label, value string, width int, theme config.Theme) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(label)+ansi.StringWidth(value)+1 > width {
		return fit(paint(label+" ", theme.Muted)+paint(value, theme.Text), width)
	}
	return paint(label, theme.Muted) + strings.Repeat(" ", width-ansi.StringWidth(label)-ansi.StringWidth(value)) + paint(value, theme.Text)
}
func statsColumns(leftLabel, leftValue, rightLabel, rightValue string, width int, theme config.Theme) string {
	gap := min(2, max(0, width))
	leftWidth := max(0, (width-gap)/2)
	rightWidth := max(0, width-gap-leftWidth)
	pair := func(label, value string, cells int) string {
		text := fit(paint(label, theme.Muted)+" "+paint(value, theme.Text), cells)
		return text + strings.Repeat(" ", max(0, cells-ansi.StringWidth(text)))
	}
	return pair(leftLabel, leftValue, leftWidth) + strings.Repeat(" ", gap) + pair(rightLabel, rightValue, rightWidth)
}
func quotaPercent(value *float64) string {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", min(100.0, max(0.0, *value)))
}
func quotaCountdown(q *SidebarQuota) string {
	if q.ResetAt.IsZero() || q.Now.IsZero() {
		return "—"
	}
	minutes := int64(math.Ceil(q.ResetAt.Sub(q.Now).Minutes()))
	if minutes < 0 {
		minutes = 0
	}
	days, hours := minutes/1440, (minutes%1440)/60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}
func quotaCredits(q *SidebarQuota) string {
	if q.UnlimitedCredits {
		return "unlimited"
	}
	if q.Credits == nil || math.IsNaN(*q.Credits) || math.IsInf(*q.Credits, 0) || *q.Credits < 0 {
		return "—"
	}
	return strconv.FormatFloat(math.Floor(*q.Credits), 'f', 0, 64)
}

// Sidebar owns its complete frame and never truncates source session data.
func Sidebar(state session.State, sessions []SessionRow, theme config.Theme, width, height int, contexts ...SidebarContext) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	ctx := SidebarContext{Directory: state.Cwd}
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if ctx.Directory == "" {
		ctx.Directory = state.Cwd
	}
	inner := max(0, width-4)
	border := func(left, right string) string {
		if width == 1 {
			return paint(left, theme.Border)
		}
		return paint(left+strings.Repeat("─", width-2)+right, theme.Border)
	}
	cell := func(s string) string {
		if width == 1 {
			return paint("│", theme.Border)
		}
		space := ""
		available := width - 2
		if width >= 4 {
			space = " "
			available = inner
		}
		s = fit(s, available)
		return paint("│", theme.Border) + space + s + strings.Repeat(" ", max(0, available-ansi.StringWidth(s))) + space + paint("│", theme.Border)
	}
	separator := func() string { return cell(paint(strings.Repeat("─", inner), theme.Border)) }
	title := func(s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text)).Bold(true).Render(s)
	}
	rows := []string{border("┌", "┐"), cell(title("SESSION")), cell(paint(oneLine(session.DisplayName(state)), theme.Text)), cell(paint(oneLine(ctx.Directory), theme.Text))}
	st := state.Stats
	cache := "—"
	denominator := float64(st.Input) + float64(st.CacheRead) + float64(st.CacheWrite)
	if denominator > 0 {
		cache = fmt.Sprintf("%.1f%%", min(100.0, max(0.0, float64(st.CacheRead)*100/denominator)))
	}
	cost := "—"
	if st.HasCost && !math.IsNaN(st.Cost) && !math.IsInf(st.Cost, 0) {
		cost = fmt.Sprintf("$%.2f", st.Cost)
	}
	rows = append(rows, cell(progressRow(state, theme, inner)), separator(), cell(title("STATS")),
		cell(statsColumns("in", number(st.Input), "out", number(st.Output), inner, theme)),
		cell(statsColumns("cache", cache, "cost", cost, inner, theme)), separator())
	if q := ctx.Quota; q != nil {
		rows = append(rows, cell(sidebarPair("Weekly limit", quotaPercent(q.RemainingPercent), inner, theme)),
			cell(sidebarPair("Resets in", quotaCountdown(q), inner, theme)),
			cell(sidebarPair("Credits", quotaCredits(q), inner, theme)))
	}
	if ctx.Mode != "" {
		value, _ := SidebarModeValue(ctx.Mode, inner)
		rows = append(rows, cell(sidebarPair("Mode", value, inner, theme)))
	}
	if ctx.Quota != nil {
		rows = append(rows, separator())
	}
	layout := SidebarGeometry(height, ctx)
	rows = append(rows, cell(title("RECENTS")), cell(""))
	historyRows := layout.HistoryCount * SidebarHistoryRows
	for row := 0; row < historyRows; row++ {
		i := layout.HistoryOffset + row/SidebarHistoryRows
		hovered := i < len(ctx.HistoryHovered) && ctx.HistoryHovered[i]
		contentWidth := inner
		scrollbar := len(ctx.History) > layout.HistoryCount && inner >= 2
		if scrollbar {
			contentWidth -= 2
		}
		depth := 0
		if i < len(ctx.HistoryDepth) {
			depth = ctx.HistoryDepth[i]
		}
		line := sidebarHistoryRow(ctx.History[i], contentWidth, hovered, depth)
		if hovered {
			line = SidebarHoverText(line)
		}
		if scrollbar {
			visibleRows := historyRows
			thumbSize := max(1, visibleRows*layout.HistoryCount/len(ctx.History))
			thumbStart := layout.HistoryOffset * (visibleRows - thumbSize) / (len(ctx.History) - layout.HistoryCount)
			mark := "│"
			if row >= thumbStart && row < thumbStart+thumbSize {
				mark = "┃"
			}
			line += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line))+1) + ScrollbarMark(mark)
		}
		rows = append(rows, cell(line))
	}
	rows = append(rows, cell(""), cell(NewSessionLabel()), cell(""))
	contentBottom := max(0, height-1)
	sessionsBottom := contentBottom
	if layout.TodoCount > 0 {
		sessionsBottom = layout.TodoRow
	}
	for _, card := range sidebarSessionCards(sessions, inner, max(0, sessionsBottom-layout.SessionsStart)) {
		rows = append(rows, cell(card))
	}
	if layout.TodoCount > 0 {
		for len(rows) < layout.TodoRow {
			rows = append(rows, cell(""))
		}
		rows = append(rows, separator())
		for row := 0; row < layout.TodoCount; row++ {
			step := ctx.Todo[layout.TodoOffset+row]
			mark, markColor, textColor := "○", theme.Muted, theme.Text
			switch step.Status {
			case "completed":
				mark, markColor, textColor = "✓", theme.Success, theme.Muted
			}
			contentWidth := inner
			scrollbar := len(ctx.Todo) > layout.TodoCount && inner >= 2
			if scrollbar {
				contentWidth -= 2
			}
			number := fmt.Sprintf("%*d.", len(fmt.Sprint(len(ctx.Todo))), layout.TodoOffset+row+1)
			body := paint(number, theme.Muted) + " " + paint(oneLine(step.Goal), textColor)
			line := " " + paint(mark, markColor) + " " + body
			if step.Status == "in_progress" || step.Status == "pending" {
				dim := theme.Dim
				if dim == "" {
					dim = theme.Muted
				}
				line = paint(" "+mark+" "+number+" "+oneLine(step.Goal), dim)
				if step.Status == "in_progress" {
					// Keep the marker and number dim; sweep only the task text.
					line = paint(" "+mark+" "+number+" ", dim) + animateTodoText(oneLine(step.Goal), ctx.TodoFrame)
				}
			}
			line = fit(line, contentWidth)
			if scrollbar {
				thumbSize := max(1, layout.TodoCount*layout.TodoCount/len(ctx.Todo))
				thumbStart := layout.TodoOffset * (layout.TodoCount - thumbSize) / (len(ctx.Todo) - layout.TodoCount)
				bar := "│"
				if row >= thumbStart && row < thumbStart+thumbSize {
					bar = "┃"
				}
				line += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line))+1) + ScrollbarMark(bar)
			}
			rows = append(rows, cell(line))
		}
	}
	for len(rows) < contentBottom {
		rows = append(rows, cell(""))
	}
	if len(rows) > contentBottom {
		rows = rows[:contentBottom]
	}
	rows = append(rows, border("└", "┘"))
	return strings.Join(frames.Gradient("sidebar", rows, width, theme), "\n")
}

// SidebarHoverText shares the RECENTS hover palette with sidebar-style popups.
func SidebarHoverText(text string) string {
	return workbenchStyle(ansi.Strip(text), workbenchAccent, false)
}

// SidebarDetailFrame uses the same text palette and frame gradient as the sidebar.
func SidebarDetailFrame(nameRows, detailRows []string, width int, theme config.Theme) []string {
	inner := max(0, width-4)
	cell := func(s, color string) string {
		s = fit(s, inner)
		return "│ " + workbenchStyle(s+strings.Repeat(" ", max(0, inner-ansi.StringWidth(s))), color, false) + " │"
	}
	rows := []string{"╭" + strings.Repeat("─", max(0, width-2)) + "╮"}
	for _, line := range nameRows {
		rows = append(rows, cell(line, workbenchText))
	}
	rows = append(rows, cell("", workbenchMuted))
	for _, line := range detailRows {
		rows = append(rows, cell(line, workbenchMuted))
	}
	rows = append(rows, "╰"+strings.Repeat("─", max(0, width-2))+"╯")
	return frames.Gradient("sidebar", rows, width, theme)
}

func number(n int) string {
	unit := ""
	value := float64(n)
	if n >= 1_000_000 {
		value /= 1_000_000
		unit = "M"
	} else if n >= 1000 {
		value /= 1000
		unit = "K"
	}
	if unit == "" {
		return fmt.Sprint(n)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + unit
}
