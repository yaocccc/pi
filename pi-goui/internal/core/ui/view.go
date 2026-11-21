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
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

// Transcript content starts at row zero; there is no duplicate session header.
const transcriptTop = 0

// The sidebar keeps its preferred edge; split details always sit immediately
// to the right of the transcript. On compact screens give details priority.
func (m *Model) paneWidths() (main, detail, sidebar int) {
	open := m.inspectorOpen()
	split := open && m.width >= 60
	if m.height >= 20 && m.width >= 96 && (!open || m.width >= 144) {
		sidebar = m.panelResize.sidebar
		if sidebar == 0 {
			sidebar = 32
		}
		reserved := 40
		if split {
			reserved += 20
		}
		sidebar = min(sidebar, m.width-reserved)
	}
	if split {
		detail = m.panelResize.inspector
		if detail == 0 {
			detail = min(60, m.width/3)
		}
		detail = min(detail, m.width-sidebar-40)
	}
	return max(1, m.width-sidebar-detail), detail, sidebar
}

func (m *Model) sidebarWidth() int { _, _, width := m.paneWidths(); return width }
func (m *Model) mainWidth() int    { width, _, _ := m.paneWidths(); return width }
func (m *Model) detailWidth() int  { _, width, _ := m.paneWidths(); return width }

// UI hit tests and pane overlays use coordinates relative to the main pane.
// Only terminal input, the hardware cursor and the final composite use screen X.
func (m *Model) mainScreenX() int {
	if m.cfg.SidebarPosition == "left" {
		return m.sidebarWidth()
	}
	return 0
}

func (m *Model) sideX() int {
	if m.cfg.SidebarPosition == "left" {
		return -m.sidebarWidth()
	}
	return m.width - m.sidebarWidth()
}

func (m *Model) sideContainsX(x, inset int) bool {
	left, width := m.sideX(), m.sidebarWidth()
	return x >= left+inset && x < left+width-inset
}
func (m *Model) resizeEditor(t *tab) {
	// The outer frame consumes two border cells and one padding cell per side.
	// Let Bubbles count its actual soft-wrapped rows and clamp its viewport after
	// edits; setting a larger fixed height alone leaves the old scroll offset.
	t.editor.Area.SetStyles(m.editorStyles())
	t.editor.Resize(max(1, m.mainWidth()-4), max(1, min(5, m.height-6)))
	t.editor.Area.Placeholder = "Message pi…  /command  @file"
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
	for i, line := range out {
		out[i] = m.paint(fitLine(" "+render.Clean(line), m.mainWidth()), m.cfg.Theme.Muted)
	}
	messages := append([]string(nil), t.state.Steering...)
	messages = append(messages, t.state.FollowUp...)
	queue := render.Queued(messages, m.hint("app.message.dequeue"), m.mainWidth(), max(4, min(9, m.height/3)), m.cfg.Theme)
	return append(queue, out...)
}
func (m *Model) transcriptHeight(t *tab) int {
	return m.transcriptHeightFor(t, len(m.completion(t)))
}

func (m *Model) transcriptHeightFor(t *tab, completionCount int) int {
	extra := len(m.extraLines(t))
	n := min(4, completionCount)
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
		completions := m.completion(t)
		height := m.transcriptHeightFor(t, len(completions))
		offset := min(max(0, t.offset), t.doc.LineCount())
		bottom := min(t.doc.LineCount(), offset+height)
		visible := t.doc.Range(offset, bottom)
		for _, row := range t.doc.ActivityRows {
			if row >= offset && row < bottom {
				visible[row-offset] = render.AnimateToolActivity(visible[row-offset], theme, m.frame)
			}
		}
		for i := range visible {
			visible[i] = m.selectionLine(t, visible[i], offset+i)
		}
		m.overlayToolInspect(visible)
		m.overlayFork(visible)
		text := strings.Join(visible, "\n")
		if len(t.state.Items) == 0 && t.promptEcho == nil && !m.showStartupInfo(t) {
			text = m.welcomeView()
		}
		rows := []string{m.transcriptViewport(t, text, t.doc.LineCount(), height)}
		if m.searching != "" {
			rows = append(rows, m.paint(fitLine(" Find: "+m.search+"▏  · Enter next / Esc close", w), theme.Accent))
		}
		for _, line := range m.extraLines(t) {
			rows = append(rows, fitLine(line, w))
		}
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
	if m.inspectorFullscreen() {
		main = m.inspectorView()
	}
	main = m.overlayFooterHover(t, main)
	main = m.overlayToast(t, main, time.Now())
	main = m.overlayPopup(t, main)
	if m.detailWidth() > 0 {
		main = joinPaneRows(main, m.inspectorView())
	}
	if sw := m.sidebarWidth(); sw > 0 {
		context := m.sidebarContext()
		layout := render.SidebarGeometry(m.height, context)
		hovered := m.sidebarSessionHoverIndex(layout)
		start := min(m.sidebarOffset, len(m.tabs))
		end := min(len(m.tabs), start+layout.Capacity)
		rows := make([]render.SessionRow, 0, end-start)
		for i, other := range m.tabs[start:end] {
			status := "idle"
			if other.resume.failed {
				status = "连接失败"
			} else if other.resumingHistory() && other.unread == 0 {
				status = "idle"
			} else if other.dead {
				status = "stopped"
			} else if len(other.dialogs) > 0 {
				status = "input"
			} else if other.cycle.phase == cycleStarting || other.resume.submit != nil || other.state.Busy || other.state.Compacting || other.sending || other.awaitingStart {
				status = "working"
			} else if n := len(other.state.Items); other.client != nil && n > 0 && other.state.Items[n-1].Role == "assistant" && other.state.Items[n-1].Error {
				status = "error"
			} else if other.unread > 0 {
				status = "done"
			}
			if other.unread > 0 {
				status += fmt.Sprintf(" +%d", other.unread)
			}
			rows = append(rows, render.SessionRow{ID: fmt.Sprint(i + m.sidebarOffset + 1), Name: session.DisplayName(other.state), Status: status, Active: other == t, Hovered: i+m.sidebarOffset == hovered})
		}
		side := m.overlayExtensions(render.Sidebar(t.state, rows, theme, sw, m.height, context))
		if m.cfg.SidebarPosition == "left" {
			main = joinPaneRows(side, main)
		} else {
			main = joinPaneRows(main, side)
		}
		main = m.overlaySidebarHistoryTooltip(t, main)
	}
	view := tea.NewView(main)
	view.Cursor = m.inputCursor(t)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeAllMotion
	view.WindowTitle = "pi · " + render.Clean(m.sessionLabel(t))
	if t.title != "" {
		view.WindowTitle = render.Clean(t.title)
	}
	// Bubble Tea negotiates CSI-u/Kitty disambiguation (Shift+Enter, Ctrl+Shift+P).
	view.KeyboardEnhancements.ReportEventTypes = false
	return view
}

func joinPaneRows(left, right string) string {
	rows, other := strings.Split(left, "\n"), strings.Split(right, "\n")
	for i := range rows {
		rows[i] += other[i]
	}
	return strings.Join(rows, "\n")
}

// inputCursor uses the same cell offsets and clipping as the rendered input.
// A painted cursor alone does not update the terminal's IME anchor.
func (m *Model) inputCursor(t *tab) *tea.Cursor {
	if m.inspectorFullscreen() && len(t.dialogs) == 0 {
		return nil
	}
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
	c.X += m.mainScreenX()
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
	if box, ok := m.sidebarHistoryTooltip(t); ok && box.contains(v.X, v.Y) {
		if box.summary.Path != "" && box.deleteRect().contains(v.X, v.Y) {
			m.sidebarHistory.tooltip = nil
			return m.requestRecentDelete(t, box.summary)
		}
		return nil
	}
	if m.beginPanelResize(v) {
		return nil
	}
	if cmd, handled := m.extensionClickCommand(v.X, v.Y); handled {
		return cmd
	}
	if cmd, handled := m.clickFooterPopup(t, v.X, v.Y); handled {
		return cmd
	}
	if m.inspectorClick(v) {
		return nil
	}
	if m.sideContainsX(v.X, 0) {
		layout := m.sidebarLayout()
		if v.Y == layout.DirectoryRow && m.sideContainsX(v.X, 2) {
			return m.openCwd(t)
		}
		historyIndex := v.Y - layout.HistoryStart
		if historyIndex >= 0 && historyIndex < layout.HistoryCount*render.SidebarHistoryRows && v.Y < m.height-1 && m.sideContainsX(v.X, 2) {
			return m.openRecent(m.sidebarHistoryAt(layout.HistoryOffset + historyIndex/render.SidebarHistoryRows).Path)
		}
		newStart, newWidth := render.SidebarNewButton(max(0, m.sidebarWidth()-4))
		newX := m.sideX() + 2 + newStart
		if v.Y == layout.NewRow && v.Y < m.height-1 && newWidth > 0 && v.X >= newX && v.X < newX+newWidth {
			return m.open("")
		}
		row := v.Y - layout.SessionsStart
		idx := row/render.SidebarSessionRows + m.sidebarOffset
		if row >= 0 && row < layout.Capacity*render.SidebarSessionRows && idx < len(m.tabs) {
			start, length := render.SidebarSessionCloseButton(max(0, m.sidebarWidth()-4))
			closeX := m.sideX() + 2 + start
			if len(t.dialogs) == 0 && row%render.SidebarSessionRows == 0 && length > 0 && v.X >= closeX && v.X < closeX+length {
				target := m.tabs[idx]
				if target.state.Busy || target.state.Compacting || target.sending || target.awaitingStart {
					focus := m.activate(idx)
					closeCmd := m.requestCloseSession(target, false)
					// A background task may already be asking a question. Show
					// the close confirmation first, preserving it on cancellation.
					if n := len(target.dialogs); n > 1 {
						confirmation := target.dialogs[n-1]
						copy(target.dialogs[1:], target.dialogs[:n-1])
						target.dialogs[0] = confirmation
					}
					return tea.Batch(focus, closeCmd, m.focus(target))
				}
				return m.requestCloseSession(target, false)
			}
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
			if d.extension.MultipleChoice() {
				cmd, _ := m.extensionKey(t, d, tea.KeyPressMsg{Code: tea.KeySpace})
				return cmd
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
	if _, rect, visible := m.latestButton(t); visible && rect.contains(v.X, v.Y) {
		t.selection = transcriptSelection{}
		t.noteScroll()
		t.follow = true
		m.clampScroll(t)
		return nil
	}
	if id, rect, ok := m.forkTarget(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		return m.requestFork(t, id)
	}
	if block, rect, ok := m.toolInspectBlock(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		if block.InspectorTask != "" {
			return m.openExtensionInspector(t, block.InspectorParent, block.InspectorTask)
		}
		m.openInspector(block.ID)
		return nil
	}
	m.beginSelection(t, v)
	return nil
}
