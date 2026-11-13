package ui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/hostui"
	"pi-goui/internal/extensions/worker"
	"pi-goui/internal/render"
	"time"
)

type workerSession = hostui.WorkerSession
type workerTranscriptMsg = hostui.WorkerTranscriptResult

var selectedWorkerDescriptor = hostui.SelectedWorkerDescriptor
var workerDescriptor = hostui.WorkerDescriptor
var workerTaskActive = hostui.WorkerTaskActive
var workerMissingRecordingReason = hostui.WorkerMissingRecordingReason
var workerRecordingErrorReason = hostui.WorkerRecordingErrorReason

func (m *Model) openWorkerSession(parent *tab, itemID, key string) tea.Cmd {
	for _, item := range parent.state.Items {
		if item.ID != itemID {
			continue
		}
		selected, title, found := render.WorkerTaskResult(item, key)
		if !found {
			return nil
		}
		d, _, valid := selectedWorkerDescriptor(selected)
		if valid && m.inspector.fallbackReason == "" && m.inspector.worker != nil && m.inspector.tabID == parent.id && m.inspector.itemID == itemID && m.inspector.taskKey == key && m.inspector.worker.Descriptor.ID == d.ID {
			return m.pollWorkerSession(time.Now())
		}
		m.finishPanelResize()
		m.inspector = toolInspector{tabID: parent.id, itemID: itemID, taskKey: key, taskTitle: title}
		var client worker.RPCClient
		if parent.client != nil {
			client = parent.client
		}
		m.inspector.worker, m.inspector.fallbackReason = hostui.NewWorkerSession(selected, parent.showThinking, client)
		parent.footerHover.active = ""
		m.codexFastHover = false
		m.resizeEditor(parent)
		m.document(parent)
		return m.pollWorkerSession(time.Now())
	}
	return nil
}

func (m *Model) pollWorkerSession(now time.Time) tea.Cmd {
	var cmds []tea.Cmd
	for page := &m.inspector; page != nil; page = page.parent {
		if cmd := m.pollWorkerPage(page, now); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}
func (m *Model) pollWorkerPage(page *toolInspector, now time.Time) tea.Cmd {
	if page.worker == nil || page.fallbackReason != "" {
		return nil
	}
	item, ok := m.inspectorItem(page)
	if !ok {
		return nil
	}
	cmd, reason := page.worker.Poll(item, now)
	if reason != "" {
		m.workerRecordingUnavailable(page, reason)
	}
	return cmd
}
func (m *Model) receiveWorkerTranscript(msg workerTranscriptMsg) tea.Cmd {
	var page *toolInspector
	for p := &m.inspector; p != nil; p = p.parent {
		if p.worker == msg.Owner {
			page = p
			break
		}
	}
	if page == nil || page.fallbackReason != "" || !m.inspectorOpen() {
		return nil
	}
	item, ok := m.inspectorItem(page)
	ptr := &item
	if !ok {
		ptr = nil
	}
	drain, reason := page.worker.Receive(msg, ptr)
	if reason != "" {
		m.workerRecordingUnavailable(page, reason)
		return nil
	}
	if drain {
		return m.pollWorkerSession(time.Now())
	}
	return nil
}
func (m *Model) workerRecordingUnavailable(page *toolInspector, reason string) {
	page.fallbackReason = reason
	if page.worker.Unavailable(reason) {
		return
	}
	page.worker = nil
	page.rows = nil
	page.cache = inspectorKey{}
	page.selection = transcriptSelection{}
}
func (m *Model) workerConversationRows(width int) []string {
	w := m.inspector.worker
	if m.inspector.rows == nil || w.Rendered != w.Revision || m.inspector.cache.width != width || m.inspector.cache.theme != m.cfg.Theme {
		m.inspector.rows = w.Rows(m.cfg.Theme, width, conversationOptions, toolInspectButton)
		m.inspector.cache.width = width
		m.inspector.cache.theme = m.cfg.Theme
		m.inspector.selection = transcriptSelection{}
	}
	bottom := max(0, len(m.inspector.rows)-m.inspectorBodyHeight())
	if !m.inspector.followPaused && !m.inspector.selection.dragging {
		m.inspector.offset = bottom
	} else {
		m.inspector.offset = min(max(0, m.inspector.offset), bottom)
	}
	return m.inspector.rows
}
