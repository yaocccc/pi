package ui

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

// A recording belongs only to the inspector, never to an editable/RPC tab.
// Its pointer is also the generation token for outstanding asynchronous reads.
type workerSession struct {
	descriptor                 session.WorkerTranscript
	state                      session.State
	inheritedMessages          int
	visible                    []session.Item
	inputExpanded              map[string]bool
	inputActions               []workerInputAction
	visibleBusy                bool
	cursor                     session.WorkerTranscriptCursor
	pending, stopped, draining bool
	next, settled              time.Time
	status                     string
	revision, rendered         uint64
	doc                        render.Document
	renderCache                render.TranscriptCache
	folds                      map[string]bool
	expanded, showThinking     bool
	targets                    []conversationTarget
	activityRows               map[int]bool
	descriptorDetails          string
	taskStatus                 string
}
type workerTranscriptMsg struct {
	owner  *workerSession
	cursor session.WorkerTranscriptCursor
	batch  session.WorkerTranscriptBatch
	err    error
}

func selectedWorkerDescriptor(item session.Item) (session.WorkerTranscript, string, bool) {
	var data struct {
		Tasks []struct {
			Transcript session.WorkerTranscript
			Status     string
		}
	}
	if json.Unmarshal(item.Details, &data) != nil || len(data.Tasks) != 1 {
		return session.WorkerTranscript{}, "", false
	}
	task := data.Tasks[0]
	return task.Transcript, task.Status, task.Transcript.Valid()
}
func workerDescriptor(item session.Item, key string) (session.WorkerTranscript, string, string, bool) {
	selected, title, ok := render.WorkerTaskResult(item, key)
	if !ok {
		return session.WorkerTranscript{}, "", "", false
	}
	d, status, valid := selectedWorkerDescriptor(selected)
	return d, title, status, valid
}
func (m *Model) openWorkerSession(parent *tab, itemID, key string) tea.Cmd {
	for _, item := range parent.state.Items {
		if item.ID != itemID {
			continue
		}
		d, title, _, valid := workerDescriptor(item, key)
		if !valid {
			parent.notify("该任务没有可用会话记录 · 旧任务无法恢复完整会话")
			return nil
		}
		if m.inspector.worker != nil && m.inspector.tabID == parent.id && m.inspector.itemID == itemID && m.inspector.taskKey == key && m.inspector.worker.descriptor.ID == d.ID {
			return m.pollWorkerSession(time.Now())
		}
		m.finishPanelResize()
		m.inspector = toolInspector{tabID: parent.id, itemID: itemID, taskKey: key, taskTitle: title, worker: &workerSession{descriptor: d, status: "正在读取", revision: 1, showThinking: parent.showThinking}}
		parent.footerHover.active = ""
		m.codexFastHover = false
		m.resizeEditor(parent)
		m.document(parent)
		return m.pollWorkerSession(time.Now())
	}
	return nil
}
func workerTaskActive(status string) bool {
	return status == "queued" || status == "running" || status == "cancelling" || status == "retrying"
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
	w := page.worker
	if w == nil || w.pending || now.Before(w.next) {
		return nil
	}
	item, ok := m.inspectorItem(page)
	if !ok {
		return nil
	}
	if w.descriptorDetails != string(item.Details) {
		d, status, valid := selectedWorkerDescriptor(item)
		if !valid || d.ID != w.descriptor.ID {
			m.closeInspector()
			return nil
		}
		w.descriptor, w.taskStatus = d, status
		w.descriptorDetails = string(item.Details)
	}
	d, status := w.descriptor, w.taskStatus
	if !d.Valid() {
		m.closeInspector()
		return nil
	}
	w.descriptor = d
	if status != "" && !workerTaskActive(status) && w.settled.IsZero() {
		w.settled = now
	}
	if w.stopped && !w.draining {
		if d.Incomplete {
			status := "Incomplete recording · " + d.Error
			w.status = status
		}
		w.next = now.Add(2 * time.Second)
		return nil
	}
	w.pending = true
	cursor, dir := w.cursor, m.cfg.AgentDir
	return func() tea.Msg {
		next, batch, err := cursor.Read(dir, d)
		return workerTranscriptMsg{w, next, batch, err}
	}
}
func (m *Model) receiveWorkerTranscript(msg workerTranscriptMsg) tea.Cmd {
	var page *toolInspector
	for p := &m.inspector; p != nil; p = p.parent {
		if p.worker == msg.owner {
			page = p
			break
		}
	}
	if page == nil || !m.inspectorOpen() {
		return nil
	}
	w := page.worker
	w.pending = false
	changed := msg.batch.Reset
	w.cursor = msg.cursor
	w.draining = msg.batch.More
	if msg.batch.Reset {
		w.state = session.State{}
		w.inheritedMessages = 0
		w.status = "实时 · 只读"
		w.stopped = false
	}
	for _, raw := range workerEventBatch(msg.batch.Records) {
		var event struct {
			Type, Cwd, Status, Error string
			Complete                 bool
		}
		_ = json.Unmarshal(raw, &event)
		switch event.Type {
		case "worker_transcript_header":
			w.state.LoadEntries(raw)
			// Header entries are inherited context, never this task's input.
			w.inheritedMessages = w.state.MessageCount
			changed = true
			w.state.Cwd = event.Cwd
		case "worker_transcript_end":
			w.status = event.Status
			w.stopped = true
			changed = changed || w.state.Busy
			w.state.Busy = false
			if !event.Complete || event.Error != "" {
				w.status = "Incomplete recording · " + event.Status + " " + event.Error
			}
		default:
			// Apply's return value requests RPC refresh; it is not a content
			// dirty flag. Message/tool events project new transcript content.
			w.state.Apply(raw)
			switch event.Type {
			case "message_start", "message_update", "message_end", "tool_execution_start", "tool_execution_update", "tool_execution_end", "agent_start", "agent_settled", "compaction_start", "compaction_end":
				changed = true
			}
		}
	}
	if w.descriptor.Incomplete {
		w.status = "Incomplete recording · " + w.descriptor.Error
		if !msg.batch.More {
			w.stopped = true
			w.state.Busy = false
		}
	}
	if msg.err != nil {
		w.draining = false
		w.status = "会话记录不可用: " + msg.err.Error()
		w.stopped = true
		w.state.Busy = false
	}
	if !w.stopped && !w.settled.IsZero() && time.Since(w.settled) > 3*time.Second && !msg.batch.More {
		w.status = "Incomplete recording · task ended without a terminal record"
		w.stopped = true
		w.state.Busy = false
	}
	if w.stopped && w.cursor.Partial() {
		w.status += " · incomplete trailing record"
	}
	if changed {
		w.refreshVisibleMessages()
	}
	w.next = time.Now().Add(500 * time.Millisecond)
	if w.draining {
		w.next = time.Time{}
		return m.pollWorkerSession(time.Now())
	}
	return nil
}

// Only the header's inherited branch is hidden. Keep complete task items so
// the shared renderer can group, fold and inspect all intermediate activity.
func (w *workerSession) refreshVisibleMessages() {
	var visible []session.Item
	for _, item := range w.state.Items {
		// Unattributed live tool events have ordinal zero until their assistant
		// snapshot arrives. Persisted header tools always have an ordinal.
		unattributed := item.MessageIndex == 0 && item.Role == "tool"
		if item.MessageIndex > w.inheritedMessages || unattributed {
			visible = append(visible, item)
		}
	}
	if w.visibleBusy == w.state.Busy && reflect.DeepEqual(w.visible, visible) {
		return
	}
	// Own mutable payloads: an in-place detail/argument update must invalidate
	// the cached document, while unchanged polling does no payload copying.
	for i := range visible {
		visible[i].Arguments = slices.Clone(visible[i].Arguments)
		visible[i].Details = slices.Clone(visible[i].Details)
		visible[i].Images = slices.Clone(visible[i].Images)
	}
	w.visible = visible
	w.visibleBusy = w.state.Busy
	w.revision++
}

func (m *Model) workerConversationRows(width int) []string {
	w := m.inspector.worker
	if m.inspector.rows == nil || w.rendered != w.revision || m.inspector.cache.width != width || m.inspector.cache.theme != m.cfg.Theme {
		view := w.state
		var inputActions []workerInputAction
		view.Items, inputActions = workerInputItems(workerReportItems(w.visible), w.inputExpanded)
		w.doc = w.renderCache.Transcript(view.Items, m.cfg.Theme, conversationOptions(view, width, w.expanded, w.showThinking, w.folds))
		w.inputActions = workerInputTargets(&w.doc, inputActions)
		m.inspector.rows = strings.Split(w.doc.Text, "\n")
		w.targets = conversationTargets(w.doc, m.inspector.rows, w.visible)
		w.activityRows = make(map[int]bool, len(w.doc.ActivityRows))
		for _, row := range w.doc.ActivityRows {
			w.activityRows[row] = true
		}
		m.inspector.cache.width = width
		m.inspector.cache.theme = m.cfg.Theme
		w.rendered = w.revision
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
