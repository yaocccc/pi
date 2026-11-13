package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/input"
	"pi-goui/internal/core/render"
	frames "pi-goui/internal/core/render/frame"
)

// popupLayout is shared by painting and mouse selection. Coordinates are screen
// cells within the main/chat region; sidebar width is deliberately excluded.
type popupLayout struct {
	x, y, width, height, inner       int
	message                          []string
	messageStart, messageTotal       int
	inputRows, optionY, count, start int
	help, textOnly, framed           bool
	optionRows                       []popupOptionRow
}

type popupOptionRow struct {
	index int
	text  string
	first bool
}

func wrappedPopupOptions(options []option, width int) []popupOptionRow {
	var rows []popupOptionRow
	for i, o := range options {
		label := render.Clean(o.Label)
		if o.Detail != "" {
			label += " · " + render.Clean(o.Detail)
		}
		for n, line := range strings.Split(ansi.Wrap(label, max(1, width-2), ""), "\n") {
			rows = append(rows, popupOptionRow{index: i, text: line, first: n == 0})
		}
	}
	return rows
}

func popupTextOnly(d *dialog) bool {
	if input, handled := d.extension.InputMode(); handled {
		return input
	}
	return d.kind == "input" || d.kind == "editor" || d.kind == "name" || d.kind == "session-rename"
}
func popupFilter(d *dialog) bool {
	if input, handled := d.extension.InputMode(); handled {
		return input
	}
	return d.kind != "confirm" && d.kind != "close" && d.kind != "resume" && d.kind != "help" && d.kind != "session-delete" && d.kind != "session-send-confirm" && d.kind != "branch-loading"
}
func popupHelpLines(d *dialog, width int) []string {
	return strings.Split(ansi.Wrap(render.Clean(d.editor.Value()), max(1, width), ""), "\n")
}

func (m *Model) popupGeometry(d *dialog) popupLayout {
	p := popupLayout{width: min(72, max(1, m.mainWidth()-4)), help: d.kind == "help", textOnly: popupTextOnly(d), framed: true}
	available := max(0, m.height-5)
	if t := m.current(); t != nil {
		available = max(0, m.editorTop(t))
	}
	if m.mainWidth() < 12 || available < 7 {
		p.width = m.mainWidth()
		p.inner = p.width
		p.height = min(available, 3)
		p.framed = false
		p.y = max(0, available-p.height)
		p.optionY = p.y
		total := len(m.filtered(d))
		if p.help {
			total = len(popupHelpLines(d, p.inner))
		}
		p.start = min(max(0, d.selected), max(0, total-1))
		p.count = min(1, p.height)
		if p.textOnly {
			p.inputRows = p.height
		}
		return p
	}
	p.inner = p.width - 4
	maxHeight := available
	if d.message != "" && (maxHeight >= 10 || d.extension.HasLongMessage()) {
		if d.extension.HasLongMessage() {
			p.message, p.messageStart, p.messageTotal = d.extension.MessageRows(d.message, p.inner, maxHeight)
		} else {
			p.message = strings.Split(ansi.Hardwrap(render.Clean(d.message), p.inner, true), "\n")
			p.messageTotal = len(p.message)
			if len(p.message) > 2 {
				p.message = p.message[:2]
				p.message[1] = ansi.Truncate(p.message[1], max(1, p.inner-1), "") + "…"
			}
		}
	}
	if popupFilter(d) {
		p.inputRows = 1
	}
	// Border + title + footer; help omits filter and uses the content budget.
	fixed := 4 + len(p.message) + p.inputRows
	if p.textOnly {
		if d.kind == "editor" {
			p.inputRows = min(10, max(1, maxHeight-4-len(p.message)))
		} else if d.extension.MultilineInput() {
			preview := input.New()
			preview.Area.SetStyles(m.editorStyles())
			preview.SetValue(d.editor.Value())
			preview.Resize(p.inner, min(5, max(1, maxHeight-4-len(p.message))))
			p.inputRows = preview.Area.Height()
		}
		p.height = 4 + len(p.message) + p.inputRows
	} else {
		budget := max(1, maxHeight-fixed-1) // one row for position/filter guidance
		wanted := len(d.options)
		if p.help {
			wanted = len(popupHelpLines(d, p.inner))
		}
		p.count = min(8, min(budget, max(1, wanted)))
		p.height = fixed + 1 + p.count
		total := len(m.filtered(d))
		if p.help {
			total = wanted
		}
		selected := min(max(0, d.selected), max(0, total-1))
		if p.help {
			p.start = min(selected, max(0, total-p.count))
		} else {
			p.start = max(0, min(selected-p.count/2, total-p.count))
		}
	}
	if d.extension.WrapOptions() && !p.textOnly {
		rows := wrappedPopupOptions(m.filtered(d), p.inner)
		budget := max(1, maxHeight-fixed-1)
		first, last := 0, 0
		for i, row := range rows {
			if row.index == d.selected {
				if row.first {
					first = i
				}
				last = i + 1
			}
		}
		// An option taller than the viewport is also exposed in the paged
		// message area, so no part of its value becomes inaccessible.
		if last-first > budget {
			message := d.message + "\n" + m.filtered(d)[d.selected].Label
			p.message, p.messageStart, p.messageTotal = d.extension.MessageRows(message, p.inner, maxHeight)
			fixed = 4 + len(p.message) + p.inputRows
			budget = max(1, maxHeight-fixed-1)
		}
		start := max(0, min(first, last-budget))
		end := min(len(rows), start+budget)
		if end > start {
			p.optionRows = rows[start:end]
			p.start = rows[start].index
			p.count = rows[end-1].index - p.start + 1
			p.height = fixed + 1 + len(p.optionRows)
		}
	}
	p.height = min(maxHeight, p.height)
	p.x = max(0, (m.mainWidth()-p.width)/2)
	p.y = max(0, available-p.height)
	p.optionY = p.y + 2 + len(p.message) + p.inputRows
	return p
}
func (m *Model) resizeDialog(d *dialog) {
	p := m.popupGeometry(d)
	d.editor.Area.SetStyles(m.editorStyles())
	if d.extension.MultilineInput() {
		d.editor.Resize(max(1, p.inner), max(1, p.inputRows))
	} else {
		d.editor.Area.SetWidth(max(1, p.inner))
		d.editor.Area.SetHeight(max(1, p.inputRows))
	}
}
func (m *Model) popupRows(d *dialog, p popupLayout) []string {
	if p.height == 0 {
		return nil
	}
	options := m.filtered(d)
	if !p.framed {
		text := render.Clean(d.title)
		if p.textOnly {
			a := d.editor.Area
			a.SetWidth(max(1, p.inner))
			a.SetHeight(p.height)
			text = a.View()
		} else if len(options) > 0 {
			text = "› " + render.Clean(options[p.start].Label)
		} else if p.help {
			lines := popupHelpLines(d, p.inner)
			text = strings.Join(lines[p.start:], "\n")
		}
		return strings.Split(fitBox(text, p.width, p.height), "\n")
	}
	title := d.title
	title = d.extension.Heading(title, p.inner)
	body := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(m.cfg.Theme.Text)).Render(render.Clean(strings.Join(strings.Fields(title), " ")))}
	for _, line := range p.message {
		body = append(body, m.paint(line, m.cfg.Theme.Muted))
	}
	if p.inputRows > 0 {
		a := d.editor.Area
		a.SetWidth(p.inner)
		a.SetHeight(p.inputRows)
		if !p.textOnly && a.Value() == "" {
			a.Placeholder = "Type to filter…"
		}
		body = append(body, strings.Split(fitBox(a.View(), p.inner, p.inputRows), "\n")...)
	}
	if !p.textOnly {
		helpLines := popupHelpLines(d, p.inner)
		for _, row := range p.optionRows {
			mark, color := "  ", m.cfg.Theme.Text
			if row.index == d.selected {
				color = m.cfg.Theme.Accent
				if row.first {
					mark = "› "
				}
			}
			body = append(body, m.paint(mark+row.text, color))
		}
		for n := 0; len(p.optionRows) == 0 && n < p.count; n++ {
			i := p.start + n
			line := ""
			if p.help {
				if i < len(helpLines) {
					line = helpLines[i]
				}
			} else if i < len(options) {
				mark, color := "  ", m.cfg.Theme.Text
				if i == min(max(0, d.selected), len(options)-1) {
					mark, color = "› ", m.cfg.Theme.Accent
				}
				o := options[i]
				label := strings.Join(strings.Fields(render.Clean(o.Label)), " ")
				if d.kind == "session" && strings.TrimSpace(d.editor.Value()) == "" && !d.reverseSort && strings.HasPrefix(strings.TrimLeft(o.Label, " "), "↳ ") {
					label = strings.Repeat(" ", min(6, len(o.Label)-len(strings.TrimLeft(o.Label, " ")))) + label
				}
				detail := strings.Join(strings.Fields(render.Clean(o.Detail)), " ")
				if d.kind == "fork" && detail != "" {
					// Keep full text in the options for filtering; shorten only
					// their presentation so question and reply are visible.
					budget := max(1, (p.inner-5)/2)
					label = ansi.Truncate(label, budget, "…")
					detail = ansi.Truncate(detail, budget, "…")
				}
				if detail != "" {
					label += " · " + detail
				}
				line = m.paint(mark+label, color)
			} else if n == 0 {
				label := "No matches"
				if d.kind == "cwd" && d.cwd != nil && d.cwd.pending {
					label = "Loading directories…"
				}
				line = m.paint(label, m.cfg.Theme.Muted)
			}
			body = append(body, line)
		}
		total := len(options)
		if p.help {
			total = len(helpLines)
		}
		hint := ""
		if total > 0 {
			hint = fmt.Sprintf("%d–%d / %d", p.start+1, min(total, p.start+p.count), total)
			if p.start > 0 {
				hint = "↑ " + hint
			}
			if p.start+p.count < total {
				hint += " ↓"
			}
		}
		body = append(body, m.paint(hint, m.cfg.Theme.Muted))
	}
	help := m.hint("tui.select.confirm") + " confirm · " + m.hint("tui.select.cancel") + " cancel"
	if d.kind == "cwd" {
		help = "↑↓ choose · " + m.hint("tui.input.tab") + " complete · " + help
	}
	help = d.extension.Guidance(help, p.messageStart, p.messageTotal, len(p.message))
	if d.extension.MultilineInput() {
		help = m.compactKey("tui.input.newLine", "ctrl+j") + " 换行 · " + help
	}
	body = append(body, m.paint(help, m.cfg.Theme.Muted))
	// Every row is clipped before framing: plugin labels cannot enlarge the box.
	rows := []string{m.paint("╭"+strings.Repeat("─", p.width-2)+"╮", m.cfg.Theme.Accent)}
	for _, line := range strings.Split(fitBox(strings.Join(body, "\n"), p.inner, p.height-2), "\n") {
		rows = append(rows, m.paint("│", m.cfg.Theme.Accent)+" "+line+" "+m.paint("│", m.cfg.Theme.Accent))
	}
	rows = append(rows, m.paint("╰"+strings.Repeat("─", p.width-2)+"╯", m.cfg.Theme.Accent))
	rows = frames.Gradient("popup", rows, p.width, m.cfg.Theme)
	if d.extension.MultilineInput() {
		if offset, total, _ := d.editor.ScrollPosition(); total > p.inputRows && p.inputRows > 0 {
			thumb := max(1, p.inputRows*p.inputRows/total)
			start := offset * (p.inputRows - thumb) / (total - p.inputRows)
			for i := start; i < start+thumb; i++ {
				row := 2 + len(p.message) + i
				edge := ansi.Cut(rows[row], p.width-1, p.width)
				rows[row] = ansi.Cut(rows[row], 0, p.width-1) + strings.Replace(edge, "│", "┃", 1)
			}
		}
	}
	return rows
}
func (m *Model) overlayPopup(t *tab, view string) string {
	if len(t.dialogs) == 0 {
		return view
	}
	d := t.dialogs[0]
	p := m.popupGeometry(d)
	rows := strings.Split(view, "\n")
	for i, line := range m.popupRows(d, p) {
		y := p.y + i
		if y >= len(rows) {
			break
		}
		rows[y] = popupBackdropSlice(rows[y], 0, p.x) + line + popupBackdropSlice(rows[y], p.x+p.width, m.mainWidth())
	}
	return strings.Join(rows, "\n")
}

// popupBackdropSlice preserves screen columns when an edge splits a wide glyph.
// ANSI cuts omit a glyph at the right edge but retain it at the left edge;
// neither half should move the popup or the remaining backdrop.
func popupBackdropSlice(s string, left, right int) string {
	if right <= left {
		return ""
	}
	start := left
	for start < right && ansi.StringWidth(ansi.Truncate(s, start, "")) < start && start < ansi.StringWidth(s) {
		start++
	}
	out := strings.Repeat(" ", start-left) + ansi.Cut(s, start, right)
	return out + strings.Repeat(" ", max(0, right-left-ansi.StringWidth(out)))
}

func (m *Model) popupOptionAt(d *dialog, x, y int) (int, bool) {
	p := m.popupGeometry(d)
	left, right := p.x, p.x+p.width
	if p.framed {
		left += 2
		right -= 2
	}
	if p.help || p.textOnly || x < left || x >= right {
		return 0, false
	}
	row := y - p.optionY
	if len(p.optionRows) > 0 {
		if row < 0 || row >= len(p.optionRows) {
			return 0, false
		}
		return p.optionRows[row].index, true
	}
	if row < 0 || row >= p.count {
		return 0, false
	}
	index := p.start + row
	return index, index < len(m.filtered(d))
}
