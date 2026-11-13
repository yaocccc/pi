package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/rpc"
	"pi-goui/internal/session"
)

type forkSnapshotMsg struct {
	id                                  int
	revision                            uint64
	path, sourceID, cwd, name, selected string
	entries                             json.RawMessage
	client                              *rpc.Client
	history                             session.ForkCheckpoint
	err                                 error
}

type forkCreatedMsg struct {
	id   int
	fork session.Fork
	err  error
}

func (m *Model) requestFork(t *tab, selected string) tea.Cmd {
	if t.forkPending {
		return nil
	}
	if t.mutation != nil || m.reservedSession(t.state.File) {
		t.notify("请等待当前会话修改完成后再创建分支")
		return nil
	}
	if t.client == nil || t.dead || m.opts.NoSession || t.state.File == "" || t.state.ID == "" {
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
	v := forkSnapshotMsg{id: t.id, revision: t.revision, path: t.state.File, sourceID: t.state.ID, cwd: t.state.Cwd, name: session.DisplayName(t.state), selected: selected, client: t.client, history: t.state.ForkCheckpoint()}
	client := t.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Never send fork/switch_session to the original process: those commands
		// replace its session and would remove the original from Active.
		v.entries, v.err = client.Call(ctx, map[string]any{"type": "get_entries"})
		return v
	}
}

func (m *Model) forkSnapshot(v forkSnapshotMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || !t.forkPending {
		return nil
	}
	if v.err != nil {
		t.forkPending = false
		t.notify("创建分支失败：" + v.err.Error())
		return nil
	}
	if t.dead || t.client == nil || t.client != v.client || m.opts.NoSession || t.state.File != v.path || t.state.ID != v.sourceID || t.mutation != nil || m.reservedSession(v.path) {
		t.forkPending = false
		t.notify("会话已变化，请重新选择分支消息")
		return nil
	}
	// Validate the selected active-branch boundary before scheduling a write.
	// This is not a UI preview: selecting a round directly creates the fork.
	plan, err := session.PreviewFork(v.selected, v.entries)
	if err != nil {
		t.forkPending = false
		t.notify("创建分支失败：" + err.Error())
		return nil
	}
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
		if !v.history.Matches(plan) || !t.state.ForkCheckpoint().Matches(plan) {
			t.forkPending = false
			t.notify("所选轮次或活动分支已变化，请重新选择分支消息")
			return nil
		}
	}
	return func() tea.Msg {
		fork, err := session.CreateFork(v.path, v.sourceID, v.cwd, v.name, v.selected, v.entries)
		return forkCreatedMsg{id: v.id, fork: fork, err: err}
	}
}

// The candidate list is presentation-only. Selection always obtains and
// validates a fresh active-branch snapshot, never this potentially stale view.
func forkChoice(t *tab, id, text string) option {
	o := option{Label: text, Value: id}
	turn, found := 0, false
	for _, item := range t.state.Items {
		if item.Role == "user" {
			if found {
				break
			}
			turn++
			if strings.TrimSuffix(item.ID, "/0") == id {
				found = true
				o.Label = fmt.Sprintf("第 %d 轮 · %s", turn, text)
				o.Detail = "回复：（无文本）"
			}
		} else if found && item.Role == "assistant" && strings.TrimSpace(item.Text) != "" {
			o.Detail = "回复：" + item.Text
		}
	}
	return o
}

func (m *Model) forkCreated(v forkCreatedMsg) tea.Cmd {
	t := m.byID(v.id)
	if t != nil {
		t.forkPending = false
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
	fork := m.addTab(v.fork.Path)
	fork.origin = v.fork.Origin
	state, _ := json.Marshal(map[string]any{"sessionId": v.fork.ID, "sessionFile": v.fork.Path, "sessionName": v.fork.Name, "cwd": v.fork.Cwd})
	_ = fork.state.LoadState(state)
	_ = fork.state.LoadEntries(v.fork.Entries)
	return tea.Batch(m.activate(len(m.tabs)-1), m.start(fork))
}
