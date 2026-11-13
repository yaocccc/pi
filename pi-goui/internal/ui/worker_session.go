package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
	"pi-goui/internal/session/worker"
)

// A recording belongs only to the inspector, never to an editable/RPC tab.
// Its pointer is also the generation token for outstanding asynchronous reads.
type workerSession struct {
	descriptor             worker.Transcript
	replay                 worker.Replay
	visible                []session.Item
	inputExpanded          map[string]bool
	inputActions           []workerInputAction
	visibleBusy            bool
	cursor                 worker.Cursor
	pending, draining      bool
	next, settled          time.Time
	revision, rendered     uint64
	doc                    render.Document
	renderCache            render.TranscriptCache
	folds                  map[string]bool
	expanded, showThinking bool
	targets                []conversationTarget
	activityRows           map[int]bool
	descriptorDetails      string
	taskStatus             string
}
type workerTranscriptMsg struct {
	owner  *workerSession
	cursor worker.Cursor
	batch  worker.Batch
	err    error
}

func selectedWorkerDescriptor(item session.Item) (worker.Transcript, string, bool) {
	var data struct {
		Tasks []struct {
			Transcript worker.Transcript
			Status     string
		}
	}
	if json.Unmarshal(item.Details, &data) != nil || len(data.Tasks) != 1 {
		return worker.Transcript{}, "", false
	}
	task := data.Tasks[0]
	return task.Transcript, task.Status, task.Transcript.Valid()
}
func workerDescriptor(item session.Item, key string) (worker.Transcript, string, string, bool) {
	selected, title, ok := render.WorkerTaskResult(item, key)
	if !ok {
		return worker.Transcript{}, "", "", false
	}
	d, status, valid := selectedWorkerDescriptor(selected)
	return d, title, status, valid
}
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
		if valid && m.inspector.fallbackReason == "" && m.inspector.worker != nil && m.inspector.tabID == parent.id && m.inspector.itemID == itemID && m.inspector.taskKey == key && m.inspector.worker.descriptor.ID == d.ID {
			return m.pollWorkerSession(time.Now())
		}
		m.finishPanelResize()
		m.inspector = toolInspector{tabID: parent.id, itemID: itemID, taskKey: key, taskTitle: title}
		if valid {
			m.inspector.worker = &workerSession{descriptor: d, replay: worker.Replay{Status: "正在读取"}, revision: 1, showThinking: parent.showThinking}
		} else {
			m.inspector.fallbackReason = workerMissingRecordingReason(selected)
		}
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
	if w == nil || page.fallbackReason != "" || w.pending || now.Before(w.next) {
		return nil
	}
	item, ok := m.inspectorItem(page)
	if !ok {
		return nil
	}
	if w.descriptorDetails != string(item.Details) {
		d, status, valid := selectedWorkerDescriptor(item)
		if !valid || d.ID != w.descriptor.ID {
			m.workerRecordingUnavailable(page, "记录关联已变化，请重新查看")
			return nil
		}
		w.descriptor, w.taskStatus = d, status
		w.descriptorDetails = string(item.Details)
	}
	d, status := w.descriptor, w.taskStatus
	if !d.Valid() {
		m.workerRecordingUnavailable(page, "记录标识无效")
		return nil
	}
	w.descriptor = d
	if status != "" && !workerTaskActive(status) && w.settled.IsZero() {
		w.settled = now
	}
	if w.replay.Stopped && !w.draining {
		w.replay.Apply(worker.ReplayRead{Descriptor: d})
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
	if page == nil || page.fallbackReason != "" || !m.inspectorOpen() {
		return nil
	}
	w := page.worker
	w.pending = false
	if item, ok := m.inspectorItem(page); ok && w.descriptorDetails != string(item.Details) {
		if d, _, valid := selectedWorkerDescriptor(item); !valid || d.ID != w.descriptor.ID {
			m.workerRecordingUnavailable(page, "记录关联已变化，请重新查看")
			return nil
		}
	}
	if msg.err != nil && msg.batch.Reset && len(w.visible) > 0 {
		m.workerRecordingUnavailable(page, workerRecordingErrorReason(msg.err))
		return nil
	}
	if w.replay.Apply(worker.ReplayRead{
		Batch: msg.batch, Descriptor: w.descriptor, Err: msg.err, Partial: msg.cursor.Partial(),
		TaskEnded: !w.settled.IsZero() && time.Since(w.settled) > 3*time.Second,
	}) {
		w.refreshVisibleMessages(w.replay.View())
	}
	if msg.err != nil {
		m.workerRecordingUnavailable(page, workerRecordingErrorReason(msg.err))
		return nil
	}
	w.cursor = msg.cursor
	w.draining = msg.batch.More
	w.next = time.Now().Add(500 * time.Millisecond)
	if w.draining {
		w.next = time.Time{}
		return m.pollWorkerSession(time.Now())
	}
	return nil
}

// Missing recordings use the existing isolated task inspector, not a fake
// conversation. Keep already-read task messages if a later read fails.
func (m *Model) workerRecordingUnavailable(page *toolInspector, reason string) {
	page.fallbackReason = reason
	if w := page.worker; w != nil && len(w.visible) > 0 {
		if w.replay.Apply(worker.ReplayRead{Err: errors.New(reason)}) {
			w.refreshVisibleMessages(w.replay.View())
		}
		w.replay.Status = "记录不完整 · " + reason + " · 保留已读取内容"
		w.pending, w.draining = false, false
		return
	}
	page.worker = nil
	page.rows = nil
	page.cache = inspectorKey{}
	page.selection = transcriptSelection{}
}

func workerMissingRecordingReason(item session.Item) string {
	var data struct{ Tasks []map[string]json.RawMessage }
	if json.Unmarshal(item.Details, &data) == nil && len(data.Tasks) == 1 {
		if raw, ok := data.Tasks[0]["transcript"]; !ok || string(raw) == "null" {
			return "该任务未保存会话记录"
		}
	}
	return "记录标识无效"
}

func workerRecordingErrorReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "记录文件不存在或已清理"
	case errors.Is(err, fs.ErrPermission):
		return "无权读取会话记录"
	default:
		return "会话记录读取失败或格式无效"
	}
}

// Cache an owned display snapshot; replay owns the conversation boundary.
func (w *workerSession) refreshVisibleMessages(view session.State) {
	visible := view.Items
	if w.visibleBusy == view.Busy && reflect.DeepEqual(w.visible, visible) {
		return
	}
	// Own mutable payloads: an in-place detail/argument update must invalidate
	// the cached document, while unchanged polling does no payload copying.
	visible = slices.Clone(visible)
	for i := range visible {
		visible[i].Arguments = slices.Clone(visible[i].Arguments)
		visible[i].Details = slices.Clone(visible[i].Details)
		visible[i].Images = slices.Clone(visible[i].Images)
	}
	w.visible = visible
	w.visibleBusy = view.Busy
	w.revision++
}

func (m *Model) workerConversationRows(width int) []string {
	w := m.inspector.worker
	if m.inspector.rows == nil || w.rendered != w.revision || m.inspector.cache.width != width || m.inspector.cache.theme != m.cfg.Theme {
		view := w.replay.View()
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
