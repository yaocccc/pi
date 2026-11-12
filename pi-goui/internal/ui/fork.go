package ui

import (
	"context"
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/session"
)

type forkSnapshotMsg struct {
	id                                  int
	revision                            uint64
	path, sourceID, cwd, name, selected string
	entries                             json.RawMessage
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
	if t.ownRunActive() || t.mutation != nil || m.reservedSession(t.state.File) {
		t.notify("请等待当前会话结束后再创建分支")
		return nil
	}
	if t.client == nil || t.dead || m.opts.NoSession || t.state.File == "" || t.state.ID == "" {
		t.notify("当前会话尚未保存，无法创建分支")
		return nil
	}
	t.forkPending = true
	v := forkSnapshotMsg{id: t.id, revision: t.revision, path: t.state.File, sourceID: t.state.ID, cwd: t.state.Cwd, name: "fork from " + session.DisplayName(t.state), selected: selected}
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
	if t.state.File != v.path || t.state.ID != v.sourceID || t.revision != v.revision || t.ownRunActive() || t.mutation != nil || m.reservedSession(v.path) {
		t.forkPending = false
		t.notify("会话已变化，请重新选择分支消息")
		return nil
	}
	return func() tea.Msg {
		fork, err := session.CreateFork(v.path, v.sourceID, v.cwd, v.name, v.selected, v.entries)
		return forkCreatedMsg{id: v.id, fork: fork, err: err}
	}
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
	state, _ := json.Marshal(map[string]any{"sessionId": v.fork.ID, "sessionFile": v.fork.Path, "sessionName": v.fork.Name, "cwd": v.fork.Cwd})
	_ = fork.state.LoadState(state)
	_ = fork.state.LoadEntries(v.fork.Entries)
	return tea.Batch(m.activate(len(m.tabs)-1), m.start(fork))
}
