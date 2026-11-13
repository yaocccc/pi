package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
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

type conversationTarget = hostui.ConversationTarget

func conversationTargets(doc render.Document, lines []string, items []session.Item) []conversationTarget {
	return hostui.ConversationTargets(doc, lines, items, toolInspectButton)
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
	for _, target := range w.Targets {
		rect := hitRect{x: target.Rect.X, y: target.Rect.Y, width: target.Rect.Width}
		rect.x += left + inset
		rect.y += 2 - m.inspector.offset
		if rect.y < 2 || rect.y >= m.height-2 || rect.x+rect.width > left+width-inset {
			continue
		}
		if nearInspectorButton(rect, x, y) {
			id, found = target.ID, rect
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
		if w.InputExpanded == nil {
			w.InputExpanded = map[string]bool{}
		}
		w.InputExpanded[key] = !w.InputExpanded[key]
		w.Revision++
		m.inspector.followPaused = true
		return nil
	}
	if id, rect, ok := m.conversationInspectTarget(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		return m.openConversationDetail(id)
	}
	point := m.inspectorPoint(v.X, v.Y)
	for _, link := range w.Doc.ImageLinks {
		if point.line == link.Row && point.cell >= link.Start && point.cell < link.End {
			for _, item := range w.Visible {
				if item.ID == link.ItemID && link.ImageIndex >= 0 && link.ImageIndex < len(item.Images) {
					return openImagePreview(m.imagePreviews, m.current().id, item.Images[link.ImageIndex])
				}
			}
			return nil
		}
	}
	if w.Folds == nil {
		w.Folds = map[string]bool{}
	}
	if toggleDocumentBlock(w.Doc, m.inspectorPoint(v.X, v.Y).line, w.Folds) {
		w.Revision++
	}
	return nil
}
func (m *Model) openConversationDetail(id string) tea.Cmd {
	w := m.inspector.worker
	if w == nil {
		return nil
	}
	itemID, key := id, ""
	for _, b := range w.Doc.Blocks {
		if b.ID == id && b.InspectorTask != "" {
			itemID, key = b.InspectorParent, b.InspectorTask
			break
		}
	}
	var child *workerSession
	title, fallbackReason := "", ""
	if key != "" {
		for _, item := range w.Visible {
			if item.ID == itemID {
				selected, t, found := render.WorkerTaskResult(item, key)
				if !found {
					return nil
				}
				title = t
				child, fallbackReason = hostui.NewWorkerSession(selected, w.ShowThinking, w.Client)
				break
			}
		}
		if child == nil && fallbackReason == "" {
			return nil
		}
	}
	old := m.inspector
	old.selection = transcriptSelection{}
	m.inspector = toolInspector{tabID: old.tabID, itemID: itemID, taskKey: key, taskTitle: title, worker: child, fallbackReason: fallbackReason, parent: &old}
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
