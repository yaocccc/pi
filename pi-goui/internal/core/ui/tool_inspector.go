package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

// One inspector belongs to one tab and one tool, not a mutable list position.
type toolInspector struct {
	parent       *toolInspector
	content      inspectorContent
	tabID        int
	itemID       string
	sourceIndex  int
	layoutSeen   bool
	offset       int
	followPaused bool
	cache        inspectorKey
	rows         render.DocumentRows
	title        string
	selection    transcriptSelection
}

type inspectorKey struct {
	name, text, args, details string
	running, failed, result   bool
	images, width             int
	theme                     config.Theme
	version                   uint64
}

// ID is the identity; cached positions are hints and may change after history reload.
func (t *tab) toolIndex(id string) (int, bool) {
	items := t.state.Items
	if i, ok := t.inspectIDs[id]; ok && i < len(items) && items[i].ID == id {
		return i, items[i].Role == "tool" || items[i].Role == "toolResult"
	}
	t.inspectIDs = make(map[string]int)
	for i := range items {
		if items[i].Role == "tool" || items[i].Role == "toolResult" {
			t.inspectIDs[items[i].ID] = i
		}
	}
	i, ok := t.inspectIDs[id]
	return i, ok
}

func (m *Model) inspectedTool() (session.Item, bool) { return m.inspectorItem(&m.inspector) }
func (m *Model) inspectorItem(page *toolInspector) (session.Item, bool) {
	t := m.current()
	if t == nil || t.id != page.tabID || page.itemID == "" {
		return session.Item{}, false
	}
	items := t.state.Items
	if page.parent != nil {
		if _, ok := m.inspectorItem(page.parent); !ok || !page.parent.view().Conversation {
			return session.Item{}, false
		}
		items = page.parent.view().Items
	}
	i := page.sourceIndex
	if i >= len(items) || items[i].ID != page.itemID {
		if page.parent == nil {
			var ok bool
			i, ok = t.toolIndex(page.itemID)
			if !ok {
				return session.Item{}, false
			}
		} else {
			for i = 0; i < len(items) && items[i].ID != page.itemID; i++ {
			}
			if i == len(items) {
				return session.Item{}, false
			}
		}
		page.sourceIndex = i
	}
	it := items[i]
	if it.Role != "tool" && it.Role != "toolResult" {
		return session.Item{}, false
	}
	if page.content != nil {
		return page.content.Resolve(it)
	}
	return it, true
}
func (m *Model) inspectorOpen() bool       { _, ok := m.inspectedTool(); return ok }
func (m *Model) inspectorFullscreen() bool { return m.inspectorOpen() && m.width < 60 }
func (m *Model) inspectorBounds() (x, width int) {
	if m.inspectorFullscreen() {
		return 0, max(1, m.width)
	}
	return m.mainWidth(), m.detailWidth()
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
	nested := m.inspector.content != nil || m.inspector.parent != nil
	m.inspector = toolInspector{}
	if t := m.current(); t != nil {
		if !nested {
			t.selection = transcriptSelection{}
		}
		m.resizeEditor(t)
		m.reflowDocument(t)
	}
}
func (m *Model) openInspector(id string) {
	m.finishPanelResize()
	t := m.current()
	if t == nil {
		return
	}
	m.inspector = toolInspector{tabID: t.id, itemID: id}
	t.selection = transcriptSelection{}
	t.footerHover.active = ""
	m.extensionControls.Hover = false
	m.resizeEditor(t)
	m.reflowDocument(t)
}
func (m *Model) inspectorInset() int {
	_, width := m.inspectorBounds()
	if width >= 4 {
		return 2
	}
	return 1
}
func (m *Model) inspectorBodyHeight() int { return max(0, m.height-4) }

// Unlike a transcript, an absent inspector page has no selectable rows.
func inspectorRowCount(rows render.DocumentRows) int {
	if rows == (render.DocumentRows{}) {
		return 0
	}
	return rows.LineCount()
}

func (m *Model) inspectorRows() render.DocumentRows {
	if m.panelResize.dragging && m.panelResize.detail {
		return m.inspector.rows
	}
	item, ok := m.inspectedTool()
	if !ok {
		return render.DocumentRows{}
	}
	if m.inspector.selection.dragging {
		return m.inspector.rows
	}
	_, width := m.inspectorBounds()
	bodyWidth := max(1, width-2*m.inspectorInset())
	if m.inspector.view().Conversation {
		return m.extensionConversationRows(bodyWidth)
	}
	key := m.inspector.cache
	var version uint64
	if m.inspector.content != nil {
		version = m.inspector.content.Version()
	}
	// Compare raw bytes to the saved strings before building a new key. The
	// string comparisons are allocation-free; converting into a new key on
	// every wheel/motion event otherwise copies the whole task snapshot.
	if key.name != item.ToolName || key.text != item.Text ||
		key.args != string(item.Arguments) || key.details != string(item.Details) ||
		key.running != item.Running || key.failed != item.Error || key.result != item.HasResult ||
		key.images != len(item.Images) || key.width != bodyWidth || key.theme != m.cfg.Theme || key.version != version {
		m.inspector.cache = inspectorKey{item.ToolName, item.Text, string(item.Arguments), string(item.Details), item.Running, item.Error, item.HasResult, len(item.Images), bodyWidth, m.cfg.Theme, version}
		if m.inspector.content != nil {
			m.inspector.rows = m.inspector.content.Rows(item, m.cfg.Theme, bodyWidth, conversationOptions, toolInspectButton)
		} else {
			m.inspector.rows = render.PlainRows(render.ToolResultRows(item, m.cfg.Theme, bodyWidth, m.extensions.Tools))
		}
		// The title may decode large source arguments too; cache it with rows.
		m.inspector.title = render.ToolResultTitle(item)
		m.inspector.selection = transcriptSelection{}
	}
	m.inspector.offset = min(max(0, m.inspector.offset), max(0, inspectorRowCount(m.inspector.rows)-m.inspectorBodyHeight()))
	return m.inspector.rows
}
func (m *Model) inspectorView() string {
	left, width := m.inspectorBounds()
	rows := m.inspectorRows()
	count := inspectorRowCount(rows)
	var out []string
	title := m.inspector.title
	if heading := m.inspector.view().Title; heading != "" {
		title = heading
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
		if row < count {
			line = rows.Line(row)
			if m.inspector.content != nil && m.inspector.content.Activity(row) {
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
		status := fmt.Sprintf("%d–%d / %d · wheel · Esc", min(count, m.inspector.offset+1), min(count, m.inspector.offset+m.inspectorBodyHeight()), count)
		if m.inspector.view().Conversation {
			status = m.inspector.view().Status + " · " + status
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
	return textPoint{line: min(max(0, m.inspector.offset+y-2), max(0, inspectorRowCount(m.inspector.rows)-1)), cell: min(max(0, x-left-m.inspectorInset()), max(0, m.inspector.cache.width-1))}
}
func (m *Model) inspectorClick(v tea.Mouse) bool {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || !m.inspectorContains(v.X, v.Y) {
		return false
	}
	if m.inspector.content == nil && m.inspector.parent == nil {
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
		if v.Y-2+m.inspector.offset < inspectorRowCount(rows) {
			p := m.inspectorPoint(v.X, v.Y)
			m.inspector.selection = transcriptSelection{anchor: p, end: p, dragging: true, document: rows}
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
			if m.inspector.view().Conversation {
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
	if !m.inspector.selection.moved && m.inspector.view().Conversation {
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
	bottom := max(0, inspectorRowCount(rows)-m.inspectorBodyHeight())
	m.inspector.offset = min(max(0, m.inspector.offset+delta), bottom)
	if m.inspector.view().Conversation && delta != 0 {
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
	b, rect, ok := m.toolInspectBlock(x, y)
	return b.ID, rect, ok
}

func blockAtOrBefore(blocks []render.Block, row int) int {
	return sort.Search(len(blocks), func(i int) bool { return blocks[i].Start > row }) - 1
}

// Render publishes blocks in row order, with child tasks immediately following
// their enclosing tool. Only the block containing this row (and its parent)
// can expose a detail action.
func (m *Model) toolInspectBlock(x, y int) (render.Block, hitRect, bool) {
	t := m.current()
	if t == nil || len(t.dialogs) > 0 || t.selection.dragging || m.inspector.selection.dragging || m.height < 8 || m.inspectorFullscreen() || x < 0 || x >= m.mainWidth() || y < transcriptTop || y >= transcriptTop+m.transcriptHeight(t) {
		return render.Block{}, hitRect{}, false
	}
	if box, ok := m.toastBox(t, time.Now()); ok && box.contains(x, y) {
		return render.Block{}, hitRect{}, false
	}
	if t.footerHover.active != "" {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if box.contains(x, y) {
			return render.Block{}, hitRect{}, false
		}
	}
	if m.extensionPopupVisible() {
		target, box, _ := m.extensionPopupGeometry()
		if target.contains(x, y) || box.contains(x, y) {
			return render.Block{}, hitRect{}, false
		}
	}
	row := t.offset + y - transcriptTop
	blocks := t.doc.Blocks
	i := blockAtOrBefore(blocks, row)
	if i < 0 || row >= blocks[i].End {
		return render.Block{}, hitRect{}, false
	}
	action, b := blocks[i], blocks[i]
	buttonRow := b.Start + 2
	if b.InspectorTask != "" {
		parent := b.InspectorParent
		i = sort.Search(i+1, func(i int) bool { return blocks[i].ID == parent || blocks[i].InspectorParent == parent })
		if i >= len(blocks) || blocks[i].ID != parent {
			return render.Block{}, hitRect{}, false
		}
		b, buttonRow = blocks[i], action.Start
	}
	top := b.Start + 1
	if !b.Expanded || (b.Role != "tool" && b.Role != "toolResult") || buttonRow <= top || row < top || row < buttonRow-1 || row > buttonRow+1 || buttonRow < t.offset || buttonRow >= t.offset+m.transcriptHeight(t) {
		return render.Block{}, hitRect{}, false
	}
	idx, ok := t.toolIndex(b.ID)
	if !ok || (action.InspectorTask == "" && m.extensions.InspectableCollection(t.state.Items[idx])) {
		return render.Block{}, hitRect{}, false
	}
	if top < 0 || max(row, buttonRow) >= min(b.End, t.doc.LineCount()) {
		return render.Block{}, hitRect{}, false
	}
	s := ansi.Strip(t.doc.Line(top))
	l, r := strings.IndexAny(s, "╭┌"), strings.LastIndexAny(s, "╮┐")
	if l < 0 || r < l {
		return render.Block{}, hitRect{}, false
	}
	left, right := ansi.StringWidth(s[:l]), ansi.StringWidth(s[:r])
	w := ansi.StringWidth(toolInspectButton)
	rect := hitRect{x: right - w, y: buttonRow - t.offset + transcriptTop, width: w}
	if right-left < 10 || x < left || x > right || right-left-1 < w || !nearInspectorButton(rect, x, y) {
		return render.Block{}, hitRect{}, false
	}
	// Cache the enclosing result frame; moving over another task in a large
	// worker collection must not repeatedly parse the card's borders.
	key := &blocks[i]
	if t.inspectFrame != key {
		t.inspectFrame, t.inspectBottom = key, top
		for j := top + 1; j < min(b.End, t.doc.LineCount()); j++ {
			corner := ansi.Cut(ansi.Strip(t.doc.Line(j)), left, left+1)
			if corner == "╰" || corner == "└" {
				t.inspectBottom = j
				break
			}
		}
	}
	if row > t.inspectBottom || buttonRow >= t.inspectBottom {
		return render.Block{}, hitRect{}, false
	}
	return action, rect, true
}
func (m *Model) overlayToolInspect(rows []string) {
	if m.panelResize.dragging || !m.sidebarHistory.pointerKnown {
		return
	}
	_, rect, ok := m.toolInspectTarget(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)
	if !ok {
		return
	}
	i := rect.y - transcriptTop
	if i < 0 || i >= len(rows) {
		return
	}
	row := rows[i]
	rows[i] = popupBackdropSlice(row, 0, rect.x) + m.paint(toolInspectButton, m.inspectorButtonColor(rect)) + popupBackdropSlice(row, rect.x+rect.width, ansi.StringWidth(row))
}
