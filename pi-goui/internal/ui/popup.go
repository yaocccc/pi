package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	frames "pi-goui/internal/render/frame"
)

// popupLayout is shared by painting and mouse selection. Coordinates are screen
// cells within the main/chat region; sidebar width is deliberately excluded.
type popupLayout struct {
	x, y, width, height, inner       int
	message                          []string
	messageStart, messageTotal       int
	inputRows, optionY, count, start int
	help, textOnly, framed           bool
}

func popupTextOnly(d *dialog) bool {
	if d.questionnaire != nil {
		return d.questionnaire.Input
	}
	return d.kind == "input" || d.kind == "editor" || d.kind == "name" || d.kind == "session-rename"
}
func popupFilter(d *dialog) bool {
	if d.questionnaire != nil {
		return d.questionnaire.Input
	}
	return d.kind != "confirm" && d.kind != "close" && d.kind != "resume" && d.kind != "help" && d.kind != "session-delete" && d.kind != "session-send-confirm"
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
			total = len(strings.Split(d.editor.Value(), "\n"))
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
	if d.message != "" && (maxHeight >= 10 || d.questionnaire != nil) {
		p.message = strings.Split(ansi.Hardwrap(render.Clean(d.message), p.inner, true), "\n")
		limit := 2
		if d.questionnaire != nil {
			limit = max(1, (maxHeight-6)/2)
		}
		p.messageTotal = len(p.message)
		if d.questionnaire != nil {
			p.messageStart = min(max(0, d.questionnaire.MessageOffset), max(0, len(p.message)-limit))
			p.message = p.message[p.messageStart:min(len(p.message), p.messageStart+limit)]
		} else if len(p.message) > limit {
			p.message = p.message[:limit]
			p.message[limit-1] = ansi.Truncate(p.message[limit-1], max(1, p.inner-1), "") + "…"
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
		}
		p.height = 4 + len(p.message) + p.inputRows
	} else {
		budget := max(1, maxHeight-fixed-1) // one row for position/filter guidance
		wanted := len(d.options)
		if p.help {
			wanted = len(strings.Split(d.editor.Value(), "\n"))
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
	p.height = min(maxHeight, p.height)
	p.x = max(0, (m.mainWidth()-p.width)/2)
	p.y = max(0, available-p.height)
	p.optionY = p.y + 2 + len(p.message) + p.inputRows
	return p
}
func (m *Model) resizeDialog(d *dialog) {
	p := m.popupGeometry(d)
	d.editor.Area.SetWidth(max(1, p.inner))
	d.editor.Area.SetHeight(max(1, p.inputRows))
	d.editor.Area.SetStyles(m.editorStyles())
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
			lines := strings.Split(render.Clean(d.editor.Value()), "\n")
			text = strings.Join(lines[p.start:], "\n")
		}
		return strings.Split(fitBox(text, p.width, p.height), "\n")
	}
	title := d.title
	if d.questionnaire != nil {
		title = d.questionnaire.tabs(p.inner)
	}
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
		helpLines := strings.Split(render.Clean(d.editor.Value()), "\n")
		for n := 0; n < p.count; n++ {
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
				if o.Detail != "" {
					label += " · " + strings.Join(strings.Fields(render.Clean(o.Detail)), " ")
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
	if d.questionnaire != nil {
		help = "←→ 切题 · ↑↓ 选择 · Enter 提交 · Esc 取消"
		if d.questionnaire.Input {
			help = "Enter 保存答案 · Esc 返回"
		} else if d.questionnaire.activeMulti() {
			help = "←→ 切题 · ↑↓ 移动 · 空格勾选 · Enter 提交"
		}
	}
	if d.questionnaire != nil && p.messageTotal > len(p.message) {
		help = fmt.Sprintf("PgUp/PgDn 翻阅问题 %d–%d/%d · Enter 提交 · Esc 返回", p.messageStart+1, p.messageStart+len(p.message), p.messageTotal)
	}
	body = append(body, m.paint(help, m.cfg.Theme.Muted))
	// Every row is clipped before framing: plugin labels cannot enlarge the box.
	rows := []string{m.paint("╭"+strings.Repeat("─", p.width-2)+"╮", m.cfg.Theme.Accent)}
	for _, line := range strings.Split(fitBox(strings.Join(body, "\n"), p.inner, p.height-2), "\n") {
		rows = append(rows, m.paint("│", m.cfg.Theme.Accent)+" "+line+" "+m.paint("│", m.cfg.Theme.Accent))
	}
	rows = append(rows, m.paint("╰"+strings.Repeat("─", p.width-2)+"╯", m.cfg.Theme.Accent))
	return frames.Gradient("popup", rows, p.width, m.cfg.Theme)
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
	if row < 0 || row >= p.count {
		return 0, false
	}
	index := p.start + row
	return index, index < len(m.filtered(d))
}
