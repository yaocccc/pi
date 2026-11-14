package hostui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"errors"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender/detail"
	"pi-goui/internal/extensions/worker"
	rpc "pi-goui/internal/runtime"
	"reflect"
	"slices"
	"strings"
	"time"
)

// A recording belongs only to the inspector, never to an editable/RPC tab.
// Its pointer is also the generation token for outstanding asynchronous reads.
type WorkerSession struct {
	Descriptor             worker.Transcript
	Replay                 worker.Replay
	Visible                []session.Item
	InputExpanded          map[string]bool
	InputActions           []WorkerInputAction
	VisibleBusy            bool
	Cursor                 worker.Cursor
	Client                 worker.RPCClient
	Pending, Draining      bool
	Next, Settled          time.Time
	Revision, Rendered     uint64
	Doc                    render.Document
	RenderCache            render.TranscriptCache
	Folds                  map[string]bool
	Expanded, ShowThinking bool
	Targets                []ConversationTarget
	ActivityRows           map[int]bool
	DescriptorDetails      string
	TaskStatus             string
	readJob                *workerTranscriptJob
}

type workerTranscriptJob struct {
	descriptor worker.Transcript
	revision   uint64
	client     worker.RPCClient
}
type WorkerTranscriptResult struct {
	Owner   *WorkerSession
	Cursor  worker.Cursor
	Batch   worker.Batch
	Err     error
	job     *workerTranscriptJob
	replay  worker.Replay
	visible []session.Item
	changed bool
	busy    bool
}

func SelectedWorkerDescriptor(item session.Item) (worker.Transcript, string, bool) {
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
func WorkerDescriptor(item session.Item, key string) (worker.Transcript, string, string, bool) {
	selected, title, ok := detail.WorkerTaskResult(item, key)
	if !ok {
		return worker.Transcript{}, "", "", false
	}
	d, status, valid := SelectedWorkerDescriptor(selected)
	return d, title, status, valid
}

func WorkerTaskActive(status string) bool {
	return status == "queued" || status == "running" || status == "cancelling" || status == "retrying"
}
func NewWorkerSession(item session.Item, thinking bool, client worker.RPCClient) (*WorkerSession, string) {
	d, _, valid := SelectedWorkerDescriptor(item)
	if !valid {
		return nil, WorkerMissingRecordingReason(item)
	}
	return &WorkerSession{Descriptor: d, Replay: worker.Replay{Status: "正在读取"}, Revision: 1, ShowThinking: thinking, Client: client}, ""
}
func (w *WorkerSession) Poll(item session.Item, now time.Time) (tea.Cmd, string) {
	if w.Pending || now.Before(w.Next) {
		return nil, ""
	}
	if w.DescriptorDetails != string(item.Details) {
		d, status, valid := SelectedWorkerDescriptor(item)
		if !valid || d.ID != w.Descriptor.ID {
			return nil, "记录关联已变化，请重新查看"
		}
		w.Descriptor, w.TaskStatus = d, status
		w.DescriptorDetails = string(item.Details)
	}
	d, status := w.Descriptor, w.TaskStatus
	if !d.Valid() {
		return nil, "记录标识无效"
	}
	if status != "" && !WorkerTaskActive(status) && w.Settled.IsZero() {
		w.Settled = now
	}
	if w.Replay.Stopped && !w.Draining {
		w.Replay.Apply(worker.ReplayRead{Descriptor: d})
		w.Next = now.Add(2 * time.Second)
		return nil, ""
	}
	w.Pending = true
	cursor, client := w.Cursor, w.Client
	job := &workerTranscriptJob{descriptor: d, revision: w.Revision, client: client}
	w.readJob = job
	prepared := w.Replay.Detached()
	previous, previousBusy := w.Visible, w.VisibleBusy
	settled := w.Settled
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		next, batch, err := cursor.Read(ctx, client, d)
		prepared.Apply(worker.ReplayRead{Batch: batch, Descriptor: d, Err: err, Partial: next.Partial(), TaskEnded: !settled.IsZero() && time.Since(settled) > 3*time.Second})
		view := prepared.View()
		changed := previousBusy != view.Busy || !reflect.DeepEqual(previous, view.Items)
		var visible []session.Item
		if changed {
			visible = ownedWorkerItems(view.Items)
		}
		if err == nil {
			err = prepared.Err
		}
		return WorkerTranscriptResult{Owner: w, Cursor: next, Batch: batch, Err: err, job: job, replay: prepared, visible: visible, changed: changed, busy: view.Busy}
	}, ""
}

// Receive is called only after the host matches the owning inspector pointer.
func (w *WorkerSession) Receive(msg WorkerTranscriptResult, item *session.Item) (drain bool, reason string) {
	if msg.Owner != w || msg.job == nil || msg.job != w.readJob {
		return false, ""
	}
	w.Pending, w.readJob = false, nil
	if w.Revision != msg.job.revision || w.Descriptor != msg.job.descriptor || reflect.ValueOf(w.Client) != reflect.ValueOf(msg.job.client) {
		w.Next = time.Time{}
		return false, ""
	}
	if item != nil && w.DescriptorDetails != string(item.Details) {
		d, _, valid := SelectedWorkerDescriptor(*item)
		if !valid || d.ID != w.Descriptor.ID {
			return false, "记录关联已变化，请重新查看"
		}
		if d != msg.job.descriptor {
			w.Next = time.Time{}
			return false, ""
		}
	}
	if msg.Err != nil {
		return false, WorkerRecordingErrorReason(msg.Err)
	}
	// No history parsing, replay or payload cloning happens during commit.
	w.Replay = msg.replay
	if msg.changed {
		w.Visible, w.VisibleBusy = msg.visible, msg.busy
		w.Revision++
	}
	w.Cursor = msg.Cursor
	w.Draining = msg.Batch.More
	w.Next = time.Now().Add(500 * time.Millisecond)
	if w.Draining {
		w.Next = time.Time{}
		return true, ""
	}
	return false, ""
}
func (w *WorkerSession) Unavailable(reason string) bool {
	if w == nil || len(w.Visible) == 0 {
		return false
	}
	// A read failure changes activity/status only. Retain the already owned
	// display payloads instead of cloning the complete recording on the UI loop.
	if w.Replay.Apply(worker.ReplayRead{Err: errors.New(reason)}) && w.VisibleBusy {
		w.VisibleBusy = false
		w.Revision++
	}
	w.Replay.Status = "记录不完整 · " + reason + " · 保留已读取内容"
	w.Pending, w.Draining, w.readJob = false, false, nil
	return true
}
func WorkerMissingRecordingReason(item session.Item) string {
	var data struct{ Tasks []map[string]json.RawMessage }
	if json.Unmarshal(item.Details, &data) == nil && len(data.Tasks) == 1 {
		if raw, ok := data.Tasks[0]["transcript"]; !ok || string(raw) == "null" {
			return "该任务未保存会话记录"
		}
	}
	return "记录标识无效"
}

func WorkerRecordingErrorReason(err error) string {
	var remote *rpc.ExtensionError
	if errors.Is(err, rpc.ErrExtensionUnavailable) {
		return "Worker 会话读取能力不可用"
	}
	if errors.As(err, &remote) {
		switch remote.Code {
		case "transcript-not-found":
			return "记录文件不存在或已清理"
		case "transcript-permission":
			return "无权读取会话记录"
		}
	}
	return "会话记录读取失败或格式无效"
}

// Cache an owned display snapshot; replay owns the conversation boundary.
func (w *WorkerSession) RefreshVisibleMessages(view session.State) {
	visible := view.Items
	if w.VisibleBusy == view.Busy && reflect.DeepEqual(w.Visible, visible) {
		return
	}
	// Own mutable payloads: an in-place detail/argument update must invalidate
	// the cached document, while unchanged polling does no payload copying.
	w.Visible = ownedWorkerItems(visible)
	w.VisibleBusy = view.Busy
	w.Revision++
}

func ownedWorkerItems(items []session.Item) []session.Item {
	items = slices.Clone(items)
	for i := range items {
		items[i].Arguments = slices.Clone(items[i].Arguments)
		items[i].Details = slices.Clone(items[i].Details)
		items[i].Images = slices.Clone(items[i].Images)
	}
	return items
}

func (w *WorkerSession) Rows(theme config.Theme, width int, options func(session.State, int, bool, bool, map[string]bool) render.Options, inspectLabel string) []string {
	view := w.Replay.View()
	var actions []WorkerInputAction
	view.Items, actions = WorkerInputItems(WorkerReportItems(w.Visible), w.InputExpanded)
	w.Doc = w.RenderCache.Transcript(view.Items, theme, options(view, width, w.Expanded, w.ShowThinking, w.Folds))
	w.InputActions = WorkerInputTargets(&w.Doc, actions)
	rows := strings.Split(w.Doc.Text, "\n")
	w.Targets = ConversationTargets(w.Doc, rows, w.Visible, inspectLabel)
	w.ActivityRows = make(map[int]bool, len(w.Doc.ActivityRows))
	for _, row := range w.Doc.ActivityRows {
		w.ActivityRows[row] = true
	}
	w.Rendered = w.Revision
	return rows
}
