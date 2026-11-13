package ui

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

// One inspector belongs to one tab and one tool, not a mutable list position.
type toolInspector struct {
	parent             *toolInspector
	worker             *workerSession
	tabID              int
	itemID             string
	taskKey, taskTitle string
	fallbackReason     string
	offset             int
	followPaused       bool
	cache              inspectorKey
	rows               []string
	title              string
	selection          transcriptSelection
	projection         inspectorTaskProjection
}

// The selected task is a derived snapshot, not UI state. Geometry/hover can ask
// whether an inspector is open dozens of times per frame: only project again
// when the source data or selected identity changes, not on every such query.
// Own the raw-byte snapshots so in-place updates invalidate this cache too.
type inspectorTaskProjection struct {
	valid, found bool
	key, title   string
	source, item session.Item
}

func (p *inspectorTaskProjection) matches(item session.Item, key string) bool {
	s := p.source
	return p.valid && p.key == key && s.ID == item.ID && s.Role == item.Role &&
		s.ToolName == item.ToolName && s.Text == item.Text && s.Running == item.Running &&
		s.Error == item.Error && s.HasResult == item.HasResult &&
		bytes.Equal(s.Arguments, item.Arguments) && bytes.Equal(s.Details, item.Details)
}

func (p *inspectorTaskProjection) resolve(item session.Item, key string) (session.Item, string, bool) {
	if !p.matches(item, key) {
		selected, title, found := render.WorkerTaskResult(item, key)
		source := item
		source.Arguments = bytes.Clone(item.Arguments)
		source.Details = bytes.Clone(item.Details)
		source.Images = nil
		*p = inspectorTaskProjection{valid: true, found: found, key: key, title: title, source: source, item: selected}
	}
	return p.item, p.title, p.found
}

type inspectorKey struct {
	name, text, args, details string
	running, failed, result   bool
	images, width             int
	theme                     config.Theme
}

func (m *Model) inspectedTool() (session.Item, bool) { return m.inspectorItem(&m.inspector) }
func (m *Model) inspectorItem(page *toolInspector) (session.Item, bool) {
	t := m.current()
	if t == nil || t.id != page.tabID || page.itemID == "" {
		return session.Item{}, false
	}
	items := t.state.Items
	if page.parent != nil {
		if _, ok := m.inspectorItem(page.parent); !ok || page.parent.worker == nil {
			return session.Item{}, false
		}
		items = page.parent.worker.visible
	}
	for _, it := range items {
		if it.ID == page.itemID && (it.Role == "tool" || it.Role == "toolResult") {
			if page.taskKey != "" {
				selected, title, ok := page.projection.resolve(it, page.taskKey)
				page.taskTitle = title
				return selected, ok
			}
			return it, true
		}
	}
	return session.Item{}, false
}
func (m *Model) inspectorOpen() bool       { _, ok := m.inspectedTool(); return ok }
func (m *Model) inspectorFullscreen() bool { return m.inspectorOpen() && m.width < 60 }
func (m *Model) inspectorBounds() (x, width int) {
	if m.inspectorFullscreen() {
		return 0, max(1, m.width)
	}
	return m.mainWidth(), m.sidebarWidth()
}
func (m *Model) inspectorContains(x, y int) bool {
	if !m.inspectorOpen() {
		return false
	}
	left, width := m.inspectorBounds()
	return x >= left && x < left+width && y >= 0 && y < m.height
}
func (m *Model) closeInspector() {
	m.finishPanelResize()
	worker := m.inspector.worker != nil || m.inspector.parent != nil || m.inspector.taskKey != ""
	m.inspector = toolInspector{}
	if t := m.current(); t != nil {
		if !worker {
			t.selection = transcriptSelection{}
		}
		m.resizeEditor(t)
		t.dirty = true
		m.document(t)
	}
}
func (m *Model) openInspector(id string) {
	m.finishPanelResize()
	t := m.current()
	if t == nil {
		return
	}
	m.inspector = toolInspector{tabID: t.id, itemID: id}
	for _, block := range t.doc.Blocks {
		if block.ID == id && block.InspectorTask != "" {
			m.inspector.itemID, m.inspector.taskKey = block.InspectorParent, block.InspectorTask
			break
		}
	}
	t.selection = transcriptSelection{}
	t.footerHover.active = ""
	m.codexFastHover = false
	m.resizeEditor(t)
	m.document(t)
}
func (m *Model) inspectorInset() int {
	_, width := m.inspectorBounds()
	if width >= 4 {
		return 2
	}
	return 1
}
func (m *Model) inspectorBodyHeight() int { return max(0, m.height-4) }
func (m *Model) inspectorRows() []string {
	if m.panelResize.dragging && m.panelResize.detail {
		return m.inspector.rows
	}
	item, ok := m.inspectedTool()
	if !ok {
		return nil
	}
	if m.inspector.selection.dragging {
		return m.inspector.rows
	}
	_, width := m.inspectorBounds()
	bodyWidth := max(1, width-2*m.inspectorInset())
	if m.inspector.worker != nil {
		return m.workerConversationRows(bodyWidth)
	}
	key := m.inspector.cache
	// Compare raw bytes to the saved strings before building a new key. The
	// string comparisons are allocation-free; converting into a new key on
	// every wheel/motion event otherwise copies the whole task snapshot.
	if m.inspector.rows == nil || key.name != item.ToolName || key.text != item.Text ||
		key.args != string(item.Arguments) || key.details != string(item.Details) ||
		key.running != item.Running || key.failed != item.Error || key.result != item.HasResult ||
		key.images != len(item.Images) || key.width != bodyWidth || key.theme != m.cfg.Theme {
		m.inspector.cache = inspectorKey{item.ToolName, item.Text, string(item.Arguments), string(item.Details), item.Running, item.Error, item.HasResult, len(item.Images), bodyWidth, m.cfg.Theme}
		if m.inspector.fallbackReason != "" {
			m.inspector.rows = render.WorkerTaskSummaryRows(item, m.inspector.fallbackReason, m.cfg.Theme, bodyWidth)
		} else {
			m.inspector.rows = render.ToolResultRows(item, m.cfg.Theme, bodyWidth)
		}
		// The title may decode large source arguments too; cache it with rows.
		m.inspector.title = render.ToolResultTitle(item)
		m.inspector.selection = transcriptSelection{}
	}
	m.inspector.offset = min(max(0, m.inspector.offset), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
	return m.inspector.rows
}
func (m *Model) inspectorView() string {
	left, width := m.inspectorBounds()
	rows := m.inspectorRows()
	var out []string
	title := m.inspector.title
	if m.inspector.taskKey != "" {
		title = "worker · " + strings.ReplaceAll(render.Clean(m.inspector.taskTitle), "\n", " ")
		if m.inspector.fallbackReason != "" {
			title = "简要详情 · " + strings.ReplaceAll(render.Clean(m.inspector.taskTitle), "\n", " ")
		}
		if m.inspector.worker != nil {
			title = "只读 · " + strings.ReplaceAll(render.Clean(m.inspector.taskTitle), "\n", " ")
		}
	}
	if m.inspector.parent != nil {
		title = "← 返回 · " + title
	}
	if rect, ok := m.inspectorCloseRect(); ok {
		title = fitLine(title, rect.x-left-m.inspectorInset()) + m.paint(inspectorCloseButton, m.inspectorButtonColor(rect))
	}
	out = append(out, title)
	_, action, showAction := m.conversationInspectTarget(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)
	for i := 0; i < m.inspectorBodyHeight(); i++ {
		row := m.inspector.offset + i
		line := ""
		if row < len(rows) {
			line = rows[row]
			if w := m.inspector.worker; w != nil && w.activityRows[row] {
				line = render.AnimateToolActivity(line, m.cfg.Theme, m.frame)
			}
			if showAction && action.y == i+2 {
				x := action.x - left - m.inspectorInset()
				line = popupBackdropSlice(line, 0, x) + m.paint(toolInspectButton, m.inspectorButtonColor(action)) + popupBackdropSlice(line, x+action.width, ansi.StringWidth(line))
			}
			if lo, hi, ok := m.inspector.selection.bounds(line, row); ok {
				line = ansi.Cut(line, 0, lo) + "\x1b[7m" + ansi.Cut(line, lo, hi) + "\x1b[27m" + ansi.Cut(line, hi, ansi.StringWidth(line))
			}
		}
		out = append(out, line)
	}
	if m.height > 3 {
		status := fmt.Sprintf("%d–%d / %d · wheel · Esc", min(len(rows), m.inspector.offset+1), min(len(rows), m.inspector.offset+m.inspectorBodyHeight()), len(rows))
		if m.inspector.worker != nil {
			status = render.Clean(m.inspector.worker.replay.Status) + " · " + status
		}
		out = append(out, m.paint(status, m.cfg.Theme.Muted))
	}
	if m.height < 3 {
		out = nil
	}
	view := strings.Join(render.ToolResultFrame(out, width, m.cfg.Theme), "\n")
	if m.height < 3 {
		return fitBox(view, width, m.height)
	}
	return view
}
func (m *Model) inspectorPoint(x, y int) textPoint {
	left, _ := m.inspectorBounds()
	return textPoint{line: min(max(0, m.inspector.offset+y-2), max(0, len(m.inspector.rows)-1)), cell: min(max(0, x-left-m.inspectorInset()), max(0, m.inspector.cache.width-1))}
}
func (m *Model) inspectorClick(v tea.Mouse) bool {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || !m.inspectorContains(v.X, v.Y) {
		return false
	}
	if m.inspector.worker == nil && m.inspector.parent == nil && m.inspector.taskKey == "" {
		t.selection = transcriptSelection{}
	}
	if rect, ok := m.inspectorCloseTarget(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		m.closeInspector()
		return true
	}
	left, width := m.inspectorBounds()
	inset := m.inspectorInset()
	if m.inspector.parent != nil && v.Y == 1 && v.X >= left+inset && v.X < left+inset+ansi.StringWidth("← 返回") {
		m.backInspector()
		return true
	}
	if v.Y >= 2 && v.Y < m.height-2 && v.X >= left+inset && v.X < left+width-inset && v.Mod&tea.ModShift == 0 {
		rows := m.inspectorRows()
		if v.Y-2+m.inspector.offset < len(rows) {
			p := m.inspectorPoint(v.X, v.Y)
			m.inspector.selection = transcriptSelection{anchor: p, end: p, dragging: true, lines: rows}
			if m.inspector.worker != nil {
				m.inspector.selection.content = m.inspector.worker.doc.Content
			}
		}
	}
	return true
}
func (m *Model) inspectorMotion(v tea.Mouse) bool {
	if !m.inspector.selection.dragging {
		return false
	}
	if t := m.current(); t == nil || len(t.dialogs) > 0 {
		m.inspector.selection = transcriptSelection{}
		return true
	}
	if v.Button == tea.MouseLeft {
		p := m.inspectorPoint(v.X, v.Y)
		if p != m.inspector.selection.anchor {
			m.inspector.selection.moved = true
			if m.inspector.worker != nil {
				m.inspector.followPaused = true
			}
		}
		m.inspector.selection.end = p
	}
	return true
}
func (m *Model) inspectorRelease(v tea.Mouse) (tea.Cmd, bool) {
	if !m.inspector.selection.dragging {
		return nil, false
	}
	if v.Button != tea.MouseLeft && v.Button != tea.MouseNone {
		return nil, true
	}
	v.Button = tea.MouseLeft // Some terminals report release with no button.
	m.inspectorMotion(v)
	m.inspector.selection.dragging = false
	if !m.inspector.selection.moved && m.inspector.worker != nil {
		m.inspector.selection = transcriptSelection{}
		return m.conversationClick(v), true
	}
	if text := m.inspector.selection.text(); text != "" {
		m.current().notify("Copied")
		return tea.SetClipboard(text), true
	}
	return nil, true
}
func (m *Model) inspectorWheel(v tea.Mouse) bool {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || !m.inspectorContains(v.X, v.Y) {
		return false
	}
	rows := m.inspectorRows()
	delta := 0
	if v.Button == tea.MouseWheelUp {
		delta = -3
	}
	if v.Button == tea.MouseWheelDown {
		delta = 3
	}
	m.inspector.selection = transcriptSelection{}
	bottom := max(0, len(rows)-m.inspectorBodyHeight())
	m.inspector.offset = min(max(0, m.inspector.offset+delta), bottom)
	if m.inspector.worker != nil && delta != 0 {
		// An upward gesture pauses even when the content currently fits.
		m.inspector.followPaused = delta < 0 || m.inspector.offset < bottom
	}
	return true
}

// Locate the actual nested result frame in the immutable transcript. No padded
// space or surrounding assistant/group frame is an inspector hit target.
const toolInspectButton = " 查看 ↗ "
const inspectorCloseButton = " 关闭 ×"

func (m *Model) inspectorButtonColor(rect hitRect) string {
	if m.sidebarHistory.pointerKnown && rect.contains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY) {
		return m.cfg.Theme.Text
	}
	return m.cfg.Theme.Muted
}

// Reveal within two horizontal cells and one row of the button; the actual
// click target remains the padded label. Callers also clip to their surface.
func nearInspectorButton(r hitRect, x, y int) bool {
	return x >= r.x-2 && x < r.x+r.width+2 && y >= r.y-1 && y <= r.y+1
}

func (m *Model) inspectorCloseTarget(x, y int) (hitRect, bool) {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || t.selection.dragging || m.inspector.selection.dragging || !m.inspectorContains(x, y) {
		return hitRect{}, false
	}
	return m.inspectorCloseRect()
}
func (m *Model) inspectorCloseRect() (hitRect, bool) {
	left, width := m.inspectorBounds()
	inset := m.inspectorInset()
	w := ansi.StringWidth(inspectorCloseButton)
	if width-2*inset < w || m.height < 3 {
		return hitRect{}, false
	}
	return hitRect{x: left + width - inset - w, y: 1, width: w}, true
}

func (m *Model) toolInspectTarget(x, y int) (string, hitRect, bool) {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || t.selection.dragging || m.inspector.selection.dragging || m.height < 8 || m.inspectorFullscreen() || x < 0 || x >= m.mainWidth() || y < m.chatTop(m.current()) || y >= m.chatTop(m.current())+m.transcriptHeight(t) {
		return "", hitRect{}, false
	}
	if box, ok := m.toastBox(t, time.Now()); ok && box.contains(x, y) {
		return "", hitRect{}, false
	}
	if t.footerHover.active != "" {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if box.contains(x, y) {
			return "", hitRect{}, false
		}
	}
	if m.codexFastPopupVisible() {
		target, box, _ := m.codexFastGeometry()
		if target.contains(x, y) || box.contains(x, y) {
			return "", hitRect{}, false
		}
	}
	lines := strings.Split(t.doc.Text, "\n")
	row := t.offset + y - m.chatTop(m.current())
	items := map[string]bool{}
	workers := map[string]bool{}
	for _, it := range t.state.Items {
		if it.Role == "tool" || it.Role == "toolResult" {
			items[it.ID] = true
			workers[it.ID] = it.ToolName == "worker" || strings.HasSuffix(it.ToolName, ".worker")
		}
	}
	type candidate struct {
		id   string
		rect hitRect
	}
	var targets []candidate
	for _, b := range t.doc.Blocks {
		top := b.Start + 1
		if !items[b.ID] || !b.Expanded || top >= len(lines) || row < top {
			continue
		}
		s := ansi.Strip(lines[top])
		l, r := strings.IndexAny(s, "╭┌"), strings.LastIndexAny(s, "╮┐")
		if l < 0 || r < l {
			continue
		}
		left, right := ansi.StringWidth(s[:l]), ansi.StringWidth(s[:r])
		if right-left < 10 || x < left || x > right {
			continue
		}
		bottom := top
		for j := top + 1; j < min(b.End, len(lines)); j++ {
			corner := ansi.Cut(ansi.Strip(lines[j]), left, left+1)
			if corner == "╰" || corner == "└" {
				bottom = j
				break
			}
		}
		if row > bottom {
			continue
		}
		w := ansi.StringWidth(toolInspectButton)
		if right-left-1 < w {
			continue
		}
		add := func(id string, buttonRow int) {
			if buttonRow <= top || buttonRow >= bottom || buttonRow < t.offset || buttonRow >= t.offset+m.transcriptHeight(t) {
				return
			}
			rect := hitRect{x: right - w, y: buttonRow - t.offset + m.chatTop(m.current()), width: w}
			if nearInspectorButton(rect, x, y) {
				targets = append(targets, candidate{id, rect})
			}
		}
		if !workers[b.ID] {
			add(b.ID, top+1)
		}
		for _, child := range t.doc.Blocks {
			if child.InspectorParent == b.ID && child.InspectorTask != "" && row >= child.Start && row < child.End {
				// Task actions sit at the end of the status/mode/preset row.
				add(child.ID, child.Start)
			}
		}
	}
	// A visible button's exact hit always wins over a neighbouring hover zone.
	// Task actions win ties with their parent, never starting fold/selection.
	for i := len(targets) - 1; i >= 0; i-- {
		if targets[i].rect.contains(x, y) {
			return targets[i].id, targets[i].rect, true
		}
	}
	if len(targets) > 0 {
		target := targets[len(targets)-1]
		return target.id, target.rect, true
	}
	return "", hitRect{}, false
}
func (m *Model) overlayToolInspect(rows []string) {
	if m.panelResize.dragging || !m.sidebarHistory.pointerKnown {
		return
	}
	_, rect, ok := m.toolInspectTarget(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)
	if !ok {
		return
	}
	i := rect.y - m.chatTop(m.current())
	if i < 0 || i >= len(rows) {
		return
	}
	row := rows[i]
	rows[i] = popupBackdropSlice(row, 0, rect.x) + m.paint(toolInspectButton, m.inspectorButtonColor(rect)) + popupBackdropSlice(row, rect.x+rect.width, ansi.StringWidth(row))
}
