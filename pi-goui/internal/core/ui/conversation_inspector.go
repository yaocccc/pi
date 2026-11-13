package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
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

func (m *Model) conversationInspectTarget(x, y int) (string, hitRect, bool) {
	view := m.inspector.view()
	if !view.Conversation || m.panelResize.dragging || m.inspector.selection.dragging || !m.sidebarHistory.pointerKnown {
		return "", hitRect{}, false
	}
	left, width := m.inspectorBounds()
	inset := m.inspectorInset()
	if x < left+inset || x >= left+width-inset || y < 2 || y >= m.height-2 {
		return "", hitRect{}, false
	}
	var id string
	var found hitRect
	for _, target := range view.Targets {
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
	view := m.inspector.view()
	if !view.Conversation || !m.inspectorContains(v.X, v.Y) || v.Y < 2 || v.Y >= m.height-2 {
		return nil
	}
	if key, ok := m.extensionInputTarget(v.X, v.Y); ok {
		m.inspector.content.ToggleInput(key)
		return nil
	}
	if id, rect, ok := m.conversationInspectTarget(v.X, v.Y); ok && rect.contains(v.X, v.Y) {
		return m.openConversationDetail(id)
	}
	point := m.inspectorPoint(v.X, v.Y)
	for _, link := range view.Document.ImageLinks {
		if point.line == link.Row && point.cell >= link.Start && point.cell < link.End {
			for _, item := range view.Items {
				if item.ID == link.ItemID && link.ImageIndex >= 0 && link.ImageIndex < len(item.Images) {
					return openImagePreview(m.imagePreviews, m.current().id, item.Images[link.ImageIndex])
				}
			}
			return nil
		}
	}
	m.inspector.content.ToggleFold(m.inspectorPoint(v.X, v.Y).line)
	return nil
}

func (m *Model) openConversationDetail(id string) tea.Cmd {
	if m.inspector.content == nil {
		return nil
	}
	itemID, content, ok := m.inspector.content.Child(id)
	if !ok {
		return nil
	}
	old := m.inspector
	old.selection = transcriptSelection{}
	m.inspector = toolInspector{tabID: old.tabID, itemID: itemID, content: content, parent: &old}
	return m.pollExtensionInspectors(time.Now())
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
