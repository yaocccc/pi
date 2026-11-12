package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

// Shared presentation policy for the editable transcript and the read-only
// recording. Only viewport state differs; neither owns a separate renderer.
func conversationOptions(state session.State, width int, expanded, thinking bool, folds map[string]bool) render.Options {
	title := state.Model.Name
	if title == "" {
		title = state.Model.ID
	}
	if title == "" {
		title = "助手"
	}
	return render.Options{Width: width, MessageCount: state.MessageCount, AssistantTitle: title, ExpandTools: expanded, ShowThinking: thinking, Expanded: folds, ShowToolActivity: state.Busy && !state.Compacting}
}

// The same effective Block state drives clicks in both transcript viewports.
func toggleDocumentBlock(doc render.Document, row int, folds map[string]bool) bool {
	for _, b := range doc.Blocks {
		if b.Foldable && row >= b.Start && row < b.End {
			folds[b.ID] = !b.Expanded
			return true
		}
	}
	return false
}

type conversationTarget struct {
	id   string
	rect hitRect
}

// Build hit regions once per document, not once per pointer-motion event. The
// coordinates are document cells; the viewport only translates and clips them.
func conversationTargets(doc render.Document, lines []string, items []session.Item) []conversationTarget {
	tools := map[string]bool{}
	workers := map[string]bool{}
	for _, it := range items {
		if it.Role == "tool" || it.Role == "toolResult" {
			tools[it.ID] = true
			workers[it.ID] = it.ToolName == "worker" || strings.HasSuffix(it.ToolName, ".worker")
		}
	}
	var out []conversationTarget
	for _, b := range doc.Blocks {
		top := b.Start + 1
		if !tools[b.ID] || !b.Expanded || top >= len(lines) {
			continue
		}
		s := ansi.Strip(lines[top])
		l, r := strings.IndexAny(s, "╭┌"), strings.LastIndexAny(s, "╮┐")
		if l < 0 || r < l {
			continue
		}
		left, right := ansi.StringWidth(s[:l]), ansi.StringWidth(s[:r])
		width := ansi.StringWidth(toolInspectButton)
		if right-left-1 < width {
			continue
		}
		if !workers[b.ID] && top+1 < b.End {
			out = append(out, conversationTarget{b.ID, hitRect{x: right - width, y: top + 1, width: width}})
		}
		if workers[b.ID] {
			for _, child := range doc.Blocks {
				if child.InspectorParent == b.ID && child.InspectorTask != "" {
					out = append(out, conversationTarget{child.ID, hitRect{x: right - width, y: child.Start, width: width}})
				}
			}
		}
	}
	return out
}
func (m *Model) conversationInspectTarget(x, y int) (string, hitRect, bool) {
	w := m.inspector.worker
	if w == nil || m.panelResize.dragging || m.inspector.selection.dragging || !m.sidebarHistory.pointerKnown {
		return "", hitRect{}, false
	}
	left, width := m.inspectorBounds()
	inset := m.inspectorInset()
	if x < left+inset || x >= left+width-inset || y < 2 || y >= m.height-2 {
		return "", hitRect{}, false
	}
	var id string
	var found hitRect
	for _, target := range w.targets {
		rect := target.rect
		rect.x += left + inset
		rect.y += 2 - m.inspector.offset
		if rect.y < 2 || rect.y >= m.height-2 || rect.x+rect.width > left+width-inset {
			continue
		}
		if nearInspectorButton(rect, x, y) {
			id, found = target.id, rect
			if rect.contains(x, y) {
				return id, found, true
			}
		}
	}
	return id, found, id != ""
}
func (m *Model) conversationClick(v tea.Mouse) tea.Cmd {
	w := m.inspector.worker
	if w == nil || !m.inspectorContains(v.X, v.Y) || v.Y < 2 || v.Y >= m.height-2 {
		return nil
	}
	if key, ok := m.workerInputTarget(v.X, v.Y); ok {
		if w.inputExpanded == nil {
			w.inputExpanded = map[string]bool{}
		}
		w.inputExpanded[key] = !w.inputExpanded[key]
		w.revision++
		m.inspector.followPaused = true
		return nil
	}
	if id, rect, ok := m.conversationInspectTarget(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		return m.openConversationDetail(id)
	}
	point := m.inspectorPoint(v.X, v.Y)
	for _, link := range w.doc.ImageLinks {
		if point.line == link.Row && point.cell >= link.Start && point.cell < link.End {
			for _, item := range w.state.Items {
				if item.ID == link.ItemID && link.ImageIndex >= 0 && link.ImageIndex < len(item.Images) {
					return openImagePreview(&m.imagePreviews, m.current().id, item.Images[link.ImageIndex])
				}
			}
			return nil
		}
	}
	if w.folds == nil {
		w.folds = map[string]bool{}
	}
	if toggleDocumentBlock(w.doc, m.inspectorPoint(v.X, v.Y).line, w.folds) {
		w.revision++
	}
	return nil
}
func (m *Model) openConversationDetail(id string) tea.Cmd {
	w := m.inspector.worker
	if w == nil {
		return nil
	}
	itemID, key := id, ""
	for _, b := range w.doc.Blocks {
		if b.ID == id && b.InspectorTask != "" {
			itemID, key = b.InspectorParent, b.InspectorTask
			break
		}
	}
	var child *workerSession
	title := ""
	if key != "" {
		for _, item := range w.state.Items {
			if item.ID == itemID {
				d, t, _, ok := workerDescriptor(item, key)
				if !ok {
					m.current().notify("该任务没有可用会话记录")
					return nil
				}
				title = t
				child = &workerSession{descriptor: d, status: "正在读取", revision: 1, showThinking: w.showThinking}
				break
			}
		}
		if child == nil {
			return nil
		}
	}
	old := m.inspector
	old.selection = transcriptSelection{}
	m.inspector = toolInspector{tabID: old.tabID, itemID: itemID, taskKey: key, taskTitle: title, worker: child, parent: &old}
	return m.pollWorkerSession(time.Now())
}
func (m *Model) backInspector() {
	if m.inspector.parent == nil {
		m.closeInspector()
		return
	}
	m.finishPanelResize()
	m.inspector = *m.inspector.parent
	m.inspector.selection = transcriptSelection{}
}
