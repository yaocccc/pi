package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

type forkSnapshotMsg struct {
	id                                  int
	revision                            uint64
	sequence                            uint64
	path, sourceID, cwd, name, selected string
	entries                             json.RawMessage
	client                              *rpc.Client
	history                             session.ForkCheckpoint
	plan                                *session.ForkPlan
	prepared                            bool
	err                                 error
}

type forkCreatedMsg struct {
	id             int
	sequence       uint64
	fork           session.Fork
	err            error
	client         *rpc.Client
	path, sourceID string
	prepared       *forkLayout
}

// Runs only in a command, against immutable captured records and RPC bytes.
func (v forkSnapshotMsg) prepare() forkSnapshotMsg {
	v.prepared = true
	if v.err != nil {
		return v
	}
	v.selected, v.err = v.history.ResolveUserID(v.selected, v.entries)
	if v.err == nil {
		v.plan, v.err = session.PrepareFork(v.selected, v.entries)
	}
	v.entries = nil // The validated plan owns the prefix; don't retain the source response.
	if v.err != nil {
		return v
	}
	plan := v.plan.Review()
	if plan.NextUserID != "" && plan.ReplyCompleted && !v.history.Matches(plan) {
		v.err = fmt.Errorf("所选轮次或活动分支已变化，请重新选择分支消息")
	}
	return v
}

func (m *Model) requestFork(t *tab, selected string) tea.Cmd {
	if t.forkPending || t.readOnly() || t.cycle.phase == cycleStarting || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.mutation != nil || m.reservedSession(t.state.File) {
		t.notify("请等待当前会话修改完成后再创建分支")
		return nil
	}
	if t.dead || m.opts.NoSession || t.state.File == "" || t.state.ID == "" {
		t.notify("当前会话尚未保存，无法创建分支")
		return nil
	}
	if t.ownRunActive() {
		for i := len(t.state.Items) - 1; i >= 0; i-- {
			item := t.state.Items[i]
			if item.Role == "user" {
				if strings.TrimSuffix(item.ID, "/0") == selected {
					t.notify("最后一轮需等待当前会话结束后才能分叉")
					return nil
				}
				break
			}
		}
	}
	t.forkPending = true
	t.forkSequence++
	v := forkSnapshotMsg{id: t.id, sequence: t.forkSequence, revision: t.revision, path: t.state.File, sourceID: t.state.ID, cwd: t.state.Cwd, name: session.DisplayName(t.state), selected: selected, client: t.client, history: t.state.ForkCheckpoint()}
	client, agentDir := t.client, m.cfg.AgentDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Never send fork/switch_session to the original process: those commands
		// replace its session and would remove the original from Active.
		if client != nil {
			v.entries, v.err = client.Call(ctx, map[string]any{"type": "get_entries"})
		} else {
			lease, err := rpc.AcquireSession(agentDir, v.path)
			if err != nil {
				v.err = err
				return v
			}
			defer lease.Close()
			v.entries, v.err = session.ReadFile(v.path)
		}
		return v.prepare()
	}
}

func (m *Model) forkSnapshot(v forkSnapshotMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || !t.forkPending || t.forkSequence != v.sequence {
		return nil
	}
	if v.err != nil {
		t.forkPending = false
		t.notify("创建分支失败：" + v.err.Error())
		return nil
	}
	if t.dead || t.client != v.client || m.opts.NoSession || t.state.File != v.path || t.state.ID != v.sourceID || t.mutation != nil || m.reservedSession(v.path) {
		t.forkPending = false
		t.notify("会话已变化，请重新选择分支消息")
		return nil
	}
	if !v.prepared {
		return func() tea.Msg { return v.prepare() }
	}
	// Parsing and the request checkpoint's content verification happened in
	// the command. The UI only checks ownership and current record versions.
	plan := v.plan.Review()
	if plan.NextUserID == "" {
		if t.ownRunActive() || t.revision != v.revision {
			t.forkPending = false
			t.notify("最后一轮需等待当前会话结束后重新选择分叉")
			return nil
		}
	} else {
		if !plan.ReplyCompleted {
			t.forkPending = false
			t.notify("所选轮次的助手回复尚未完成，无法分叉")
			return nil
		}
		// Compare the immutable copied history, not the session revision:
		// later streaming events can change revision without touching this turn.
		current := t.state.ForkCheckpoint()
		if !current.SamePrefix(v.history, plan) {
			// A prefix version changed while preparing. Recheck its content
			// off-thread; unrelated later streaming keeps SamePrefix true.
			return func() tea.Msg {
				if !current.Matches(plan) {
					v.err = fmt.Errorf("所选轮次或活动分支已变化，请重新选择分支消息")
				}
				v.history = current
				return v
			}
		}
	}
	root := m.cfg.AgentDir
	readLayout := m.forkLayoutReader(v.cwd)
	return func() tea.Msg {
		fork, err := v.plan.PersistNumbered(root, v.path, v.sourceID, v.cwd, v.name)
		result := forkCreatedMsg{id: v.id, sequence: v.sequence, fork: fork, err: err, client: v.client, path: v.path, sourceID: v.sourceID}
		if err == nil {
			result.prepared, result.err = readLayout(fork)
		}
		return result
	}
}

// The candidate list is presentation-only. Selection always obtains and
// validates a fresh active-branch snapshot, never this potentially stale view.
type forkChoiceInfo struct {
	turn  int
	reply string
}

// Scan once per chooser. Each row keeps only the latest nonblank assistant
// reply in its turn, just as the individual user-header action does.
func forkChoices(items []session.Item) map[string]forkChoiceInfo {
	rows := make(map[string]forkChoiceInfo)
	var id string
	var turn int
	for _, item := range items {
		if item.Role == "user" {
			id = strings.TrimSuffix(item.ID, "/0")
			turn++
			rows[id] = forkChoiceInfo{turn: turn}
		} else if id != "" && item.Role == "assistant" && strings.TrimSpace(item.Text) != "" {
			rows[id] = forkChoiceInfo{turn, item.Text}
		}
	}
	return rows
}

func (info forkChoiceInfo) choice(id, text string) option {
	if info.turn == 0 {
		return option{Value: id, Label: text}
	}
	detail := "回复：（无文本）"
	if info.reply != "" {
		detail = "回复：" + info.reply
	}
	return option{Value: id, Label: fmt.Sprintf("第 %d 轮 · %s", info.turn, text), Detail: detail}
}

func (m *Model) forkCreated(v forkCreatedMsg) tea.Cmd {
	t := m.byID(v.id)
	if v.sourceID != "" {
		if t == nil || !t.forkPending || t.forkSequence != v.sequence {
			return nil
		}
		if t.client != v.client || t.state.File != v.path || t.state.ID != v.sourceID || t.dead {
			// The durable fork remains available in resume; a stale result
			// cannot open a tab for a closed/replaced source backend.
			t.forkPending = false
			return nil
		}
	}
	if t != nil {
		t.forkPending = false
	}
	if v.err == nil && v.prepared == nil {
		v.err = fmt.Errorf("fork history was not prepared")
	}
	if v.err != nil {
		if t == nil {
			t = m.current()
		}
		if t != nil {
			t.notify("创建分支失败：" + v.err.Error())
		}
		return nil
	}
	// A fresh tab has no source live/tool overlays, errors, run state or render
	// caches. The source tab and RPC writer remain attached to their old session.
	fork, openCmd, err := m.openForkLayout(t, v.fork, v.prepared)
	if err != nil {
		if t != nil {
			t.notify("创建分支失败：" + err.Error())
		}
		return nil
	}
	// Publish the durable file immediately, without waiting for RPC startup or
	// the five-second history poll. Invalidate any pre-creation scan in flight.
	h := &m.sidebarHistory
	h.generation++
	h.pending = false
	h.checked = time.Time{}
	cwd := m.tabDirectory(fork)
	if h.cwd != cwd {
		h.sessions = nil
		h.offset = 0
	}
	h.cwd = cwd
	saved := session.Summary{ID: v.fork.ID, Path: v.fork.Path, Name: v.fork.Name, Cwd: v.fork.Cwd, Modified: time.Now().UnixMilli(), ParentSession: v.fork.Origin.ParentSession, Fork: v.fork.Origin.Fork}
	list := []session.Summary{saved}
	for _, previous := range h.sessions {
		if !sameSessionPath(previous.Path, saved.Path) {
			list = append(list, previous)
		}
	}
	h.sessions = list
	return tea.Batch(openCmd, m.requestSidebarHistory(time.Now()))
}
