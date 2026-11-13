package hostui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"errors"
	"pi-goui/internal/config"
	"pi-goui/internal/extensions/worker"
	"pi-goui/internal/render"
	"pi-goui/internal/rpc"
	"pi-goui/internal/session"
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
}
type WorkerTranscriptResult struct {
	Owner  *WorkerSession
	Cursor worker.Cursor
	Batch  worker.Batch
	Err    error
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
	selected, title, ok := render.WorkerTaskResult(item, key)
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
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		next, batch, err := cursor.Read(ctx, client, d)
		return WorkerTranscriptResult{w, next, batch, err}
	}, ""
}

// Receive is called only after the host matches the owning inspector pointer.
func (w *WorkerSession) Receive(msg WorkerTranscriptResult, item *session.Item) (drain bool, reason string) {
	w.Pending = false
	if item != nil && w.DescriptorDetails != string(item.Details) {
		if d, _, valid := SelectedWorkerDescriptor(*item); !valid || d.ID != w.Descriptor.ID {
			return false, "记录关联已变化，请重新查看"
		}
	}
	if msg.Err != nil && msg.Batch.Reset && len(w.Visible) > 0 {
		return false, WorkerRecordingErrorReason(msg.Err)
	}
	if w.Replay.Apply(worker.ReplayRead{Batch: msg.Batch, Descriptor: w.Descriptor, Err: msg.Err, Partial: msg.Cursor.Partial(), TaskEnded: !w.Settled.IsZero() && time.Since(w.Settled) > 3*time.Second}) {
		w.RefreshVisibleMessages(w.Replay.View())
	}
	if msg.Err != nil {
		return false, WorkerRecordingErrorReason(msg.Err)
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
	if w.Replay.Apply(worker.ReplayRead{Err: errors.New(reason)}) {
		w.RefreshVisibleMessages(w.Replay.View())
	}
	w.Replay.Status = "记录不完整 · " + reason + " · 保留已读取内容"
	w.Pending, w.Draining = false, false
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
	visible = slices.Clone(visible)
	for i := range visible {
		visible[i].Arguments = slices.Clone(visible[i].Arguments)
		visible[i].Details = slices.Clone(visible[i].Details)
		visible[i].Images = slices.Clone(visible[i].Images)
	}
	w.Visible = visible
	w.VisibleBusy = view.Busy
	w.Revision++
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
