package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

// Transcript content starts at row zero; there is no duplicate session header.
const transcriptTop = 0

func (m *Model) sidebarWidth() int {
	if m.width < 96 || m.height < 20 {
		return 0
	}
	return 32
}
func (m *Model) mainWidth() int { return max(1, m.width-m.sidebarWidth()) }
func (m *Model) resizeEditor(t *tab) {
	// The outer frame consumes two border cells and one padding cell per side.
	// Let Bubbles count its actual soft-wrapped rows and clamp its viewport after
	// edits; setting a larger fixed height alone leaves the old scroll offset.
	t.editor.Area.SetStyles(m.editorStyles())
	t.editor.Resize(max(1, m.mainWidth()-4), max(1, min(7, m.height/4)))
	t.editor.Area.Placeholder = "Message pi…  / for commands"
	for _, d := range t.dialogs {
		m.resizeDialog(d)
	}
	m.clampScroll(t)
}

// Default textarea styles paint the cursor's entire row, including modal inputs.
// Keep only the transient cursor/selection highlight; all surfaces are transparent.
func (m *Model) editorStyles() textarea.Styles {
	s := textarea.DefaultLightStyles()
	s.Focused.Base = lipgloss.NewStyle().Foreground(lipgloss.Color(m.cfg.Theme.Text))
	s.Focused.Text = s.Focused.Base
	s.Focused.CursorLine = lipgloss.NewStyle()
	s.Blurred = s.Focused
	return s
}
func (m *Model) extraLines(t *tab) []string {
	var out []string
	q := len(t.state.Steering) + len(t.state.FollowUp)
	if q > 0 {
		out = append(out, fmt.Sprintf("↳ queued %d · %s restores", q, m.hint("app.message.dequeue")))
	}
	keys := make([]string, 0, len(t.widgets))
	for k := range t.widgets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, t.widgets[k]...)
	}
	if len(out) > 3 {
		out = append(out[:2], fmt.Sprintf("… %d more widget lines", len(out)-2))
	}
	return out
}
func (m *Model) transcriptHeight(t *tab) int {
	extra := len(m.extraLines(t))
	n := min(4, len(m.completion(t)))
	if m.searching != "" {
		n++
	}
	return max(1, m.height-transcriptTop-1-m.editorFrameHeight(t)-extra-n)
}
func (m *Model) hint(action string) string {
	keys := m.cfg.Keys.Keys(action)
	if len(keys) == 0 {
		return "unbound"
	}
	return keys[0]
}
func (m *Model) paint(s, color string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}
func fitLine(s string, w int) string {
	s = ansi.Truncate(s, max(1, w), "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}
func fitBox(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, max(0, h))
	for i := range out {
		if i < len(lines) {
			out[i] = fitLine(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", max(0, w))
		}
	}
	return strings.Join(out, "\n")
}
func (m *Model) View() tea.View {
	t := m.current()
	if t == nil {
		return tea.NewView("")
	}
	w := m.mainWidth()
	theme := m.cfg.Theme
	// Blur only the rendered copy; popup focus must not modify the draft.
	backdrop := *t
	if len(t.dialogs) > 0 {
		backdrop.editor.Area.Blur()
	}
	var main string
	if m.height < 8 {
		// Keep the input/selection accessible when there is no room for chrome.
		body := backdrop.editor.Area.View()
		rows := strings.Split(m.workingLine(t)+"\n"+body, "\n")
		if len(rows) > m.height {
			rows = rows[len(rows)-m.height:]
		}
		main = fitBox(strings.Join(rows, "\n"), w, m.height)
	} else {
		height := m.transcriptHeight(t)
		lines := strings.Split(t.doc.Text, "\n")
		offset := min(max(0, t.offset), len(lines))
		bottom := min(len(lines), offset+height)
		visible := append([]string(nil), lines[offset:bottom]...)
		for i := range visible {
			visible[i] = m.selectionLine(t, visible[i], offset+i)
		}
		text := strings.Join(visible, "\n")
		if len(t.state.Items) == 0 {
			text = m.paint("\n  A quiet place to work.\n\n  Ask pi a question, or use / to explore commands.\n  Create a second session with /new; both can run in parallel.", theme.Muted)
		}
		var rows []string
		rows = append(rows, fitBox(text, w, height))
		if m.searching != "" {
			rows = append(rows, m.paint(fitLine(" Find: "+m.search+"▏  · Enter next / Esc close", w), theme.Accent))
		}
		for _, line := range m.extraLines(t) {
			rows = append(rows, m.paint(fitLine(" "+render.Clean(line), w), theme.Muted))
		}
		completions := m.completion(t)
		start := max(0, min(m.completionIndex-2, len(completions)-4))
		for i := start; i < min(len(completions), start+4); i++ {
			prefix := "  "
			color := theme.Muted
			if i == m.completionIndex {
				prefix = "› "
				color = theme.Accent
			}
			c := completions[i]
			rows = append(rows, m.paint(fitLine(prefix+m.completionLabel(t, c)+"  "+render.Clean(c.Description), w), color))
		}
		// Keep the frame and working row anchored even when widgets/completions
		// consume the available space on a short terminal.
		top := fitBox(strings.Join(rows, "\n"), w, max(0, m.editorTop(t)-1))
		main = top + "\n" + m.statusLine(t, m.workingLine(t)) + "\n" + m.editorFrame(&backdrop)
	}
	main = fitBox(main, w, m.height)
	main = m.overlayToast(t, main, time.Now())
	main = m.overlayPopup(t, main)
	if sw := m.sidebarWidth(); sw > 0 {
		rows := make([]render.SessionRow, 0, len(m.tabs))
		for i, other := range m.tabs[m.sidebarOffset:] {
			status := "idle"
			if other.dead {
				status = "stopped"
			} else if len(other.dialogs) > 0 {
				status = "input"
			} else if other.client == nil {
				status = "starting"
			} else if other.state.Busy || other.state.Compacting || other.sending || other.awaitingStart {
				status = "working"
			} else if n := len(other.state.Items); n > 0 && other.state.Items[n-1].Role == "assistant" && other.state.Items[n-1].Error {
				status = "error"
			} else if other.unread > 0 {
				status = "done"
			}
			if other.unread > 0 {
				status += fmt.Sprintf(" +%d", other.unread)
			}
			rows = append(rows, render.SessionRow{ID: fmt.Sprint(i + m.sidebarOffset + 1), Name: session.DisplayName(other.state), Status: status, Active: other == t})
		}
		side := render.Sidebar(t.state, rows, theme, sw, m.height, m.sidebarContext())
		mainLines, sideLines := strings.Split(main, "\n"), strings.Split(side, "\n")
		for i := range mainLines {
			mainLines[i] += sideLines[i]
		}
		main = strings.Join(mainLines, "\n")
	}
	view := tea.NewView(main)
	view.Cursor = m.inputCursor(t)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "pi · " + render.Clean(m.sessionLabel(t))
	if t.title != "" {
		view.WindowTitle = render.Clean(t.title)
	}
	// Bubble Tea negotiates CSI-u/Kitty disambiguation (Shift+Enter, Ctrl+Shift+P).
	view.KeyboardEnhancements.ReportEventTypes = false
	return view
}

// inputCursor uses the same cell offsets and clipping as the rendered input.
// A painted cursor alone does not update the terminal's IME anchor.
func (m *Model) inputCursor(t *tab) *tea.Cursor {
	a := t.editor.Area
	x, y := 2, m.editorTop(t)+1
	width, height := max(1, m.mainWidth()-4), m.editorContentHeight(t)
	if len(t.dialogs) > 0 {
		d := t.dialogs[0]
		p := m.popupGeometry(d)
		if p.inputRows == 0 || p.height == 0 {
			return nil
		}
		a = d.editor.Area
		a.SetWidth(max(1, p.inner))
		a.SetHeight(max(1, p.inputRows))
		x, y, width, height = p.x, p.y, p.inner, p.inputRows
		if p.framed {
			x += 2
			y += 2 + len(p.message)
		}
	} else if m.searching != "" {
		return nil
	} else if m.height < 8 {
		x, y = 0, 1-max(0, 1+len(strings.Split(a.View(), "\n"))-m.height)
		width = m.mainWidth()
	}
	c := a.Cursor()
	if c == nil || c.X < 0 || c.X >= width || c.Y < 0 || c.Y >= height {
		return nil
	}
	c.X += x
	c.Y += y
	if c.X < 0 || c.X >= m.mainWidth() || c.Y < 0 || c.Y >= m.height {
		return nil
	}
	return c
}

func (m *Model) mouse(v tea.Mouse) tea.Cmd {
	if v.Button != tea.MouseLeft {
		return nil
	}
	t := m.current()
	if t == nil {
		return nil
	}
	if m.sidebarWidth() > 0 && v.X >= m.mainWidth() {
		layout := m.sidebarLayout()
		if v.Y == layout.DirectoryRow && v.X >= m.mainWidth()+2 && v.X < m.width-2 {
			return m.openCwd(t)
		}
		if v.Y == layout.NewRow {
			return m.open("")
		}
		row := v.Y - layout.SessionsStart
		idx := row/render.SidebarSessionRows + m.sidebarOffset
		if row >= 0 && row < layout.Capacity*render.SidebarSessionRows && idx < len(m.tabs) {
			return m.activate(idx)
		}
		return nil
	}
	if len(t.dialogs) > 0 {
		d := t.dialogs[0]
		if index, ok := m.popupOptionAt(d, v.X, v.Y); ok {
			d.selected = index
			if d.kind == "cwd" {
				return m.completeCwd(d)
			}
			if d.questionnaire != nil && d.questionnaire.activeMulti() {
				return m.questionnaireKey(t, d, tea.KeyPressMsg{Code: tea.KeySpace})
			}
			return m.confirmDialog(t, d)
		}
		return nil
	}
	if box, ok := m.toastBox(t, time.Now()); ok && box.contains(v.X, v.Y) {
		t.toast = toastState{}
		t.selection.dragging = false
		return nil
	}
	footer := m.inputFooter(t)
	if footer.visible {
		if footer.model.contains(v.X, v.Y) {
			return call(t, "get_available_models", nil)
		}
		if footer.thinking.contains(v.X, v.Y) {
			return call(t, "cycle_thinking_level", nil)
		}
	}
	if _, rect, visible := m.latestButton(t); visible && rect.contains(v.X, v.Y) {
		t.selection = transcriptSelection{}
		t.follow = true
		m.clampScroll(t)
		return nil
	}
	m.beginSelection(t, v)
	return nil
}
