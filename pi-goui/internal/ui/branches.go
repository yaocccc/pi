package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

type branchJump struct {
	path, id, entry string
	epoch           uint64
	loading         *dialog
}

type branchRequest struct {
	owner           int
	epoch, revision uint64
	path, id        string
	loading         *dialog
}

type branchOpenMsg struct {
	branchRequest
	target session.Summary
	header session.Header
	entry  string
	err    error
}

func (m *Model) branchRequest(t *tab) (branchRequest, bool) {
	if t == nil || m.opts.NoSession || t.state.File == "" || t.state.ID == "" || t.mutation != nil || m.reservedSession(t.state.File) {
		if t != nil {
			t.notify("此会话尚未保存或正在修改，无法查看来源")
		}
		return branchRequest{}, false
	}
	// The newest navigation intent wins, even if two disk reads finish out of order.
	m.branchEpoch++
	// Superseded reads must not leave an inaccessible loading dialog behind.
	for _, other := range m.tabs {
		for i := len(other.dialogs) - 1; i >= 0; i-- {
			if other.dialogs[i].kind == "branch-loading" {
				other.dialogs = append(other.dialogs[:i], other.dialogs[i+1:]...)
			}
		}
		other.originJump = nil
	}
	loading := m.branchLoading(t, "读取来源")
	return branchRequest{owner: t.id, epoch: m.branchEpoch, revision: t.revision, path: t.state.File, id: t.state.ID, loading: loading}, true
}

func (m *Model) branchLoading(t *tab, title string) *dialog {
	d := m.newDialog("branch-loading", title, "Esc 取消；不会修改任何会话")
	d.options = []option{{Label: "取消"}}
	t.dialogs = append(t.dialogs, d)
	return d
}

// Remove only the dialog belonging to this result, even if the owner is stale.
// A missing dialog means that its operation was cancelled or superseded.
func removeBranchLoading(t *tab, loading *dialog) bool {
	if loading == nil {
		return true
	}
	if t != nil {
		for i, d := range t.dialogs {
			if d == loading {
				t.dialogs = append(t.dialogs[:i], t.dialogs[i+1:]...)
				return true
			}
		}
	}
	return false
}

func (m *Model) branchOwner(r branchRequest) *tab {
	t := m.byID(r.owner)
	if !removeBranchLoading(t, r.loading) {
		return nil
	}
	if t == nil || t != m.current() || r.epoch != m.branchEpoch || t.state.ID != r.id || t.state.File != r.path || t.revision != r.revision || t.mutation != nil || m.reservedSession(r.path) || len(t.dialogs) > 0 {
		return nil
	}
	return t
}

func branchName(s session.Summary) string {
	name := s.Name
	if name == "" {
		name = s.FirstMessage
	}
	if name == "" {
		name = filepath.Base(s.Path)
	}
	return strings.Join(strings.Fields(render.Clean(name)), " ")
}

func summaryOf(t *tab) session.Summary {
	v := session.Summary{ID: t.state.ID, Path: t.state.File, Cwd: t.state.Cwd, Name: session.DisplayName(t.state)}
	if t.origin.ID == t.state.ID {
		v.ParentSession, v.Fork = t.origin.ParentSession, t.origin.Fork
	}
	return v
}

func sameSessionPath(a, b string) bool {
	return a != "" && b != "" && filepath.Clean(a) == filepath.Clean(b)
}

func (m *Model) knownBranches() []session.Summary {
	out := []session.Summary{}
	seen := map[string]bool{}
	add := func(s session.Summary) {
		if s.Path != "" && !seen[s.Path] {
			seen[s.Path] = true
			out = append(out, s)
		}
	}
	// Live titles take precedence over cached disk names.
	for _, t := range m.tabs {
		add(summaryOf(t))
	}
	for _, s := range m.sidebarHistory.sessions {
		add(s)
	}
	return out
}

func (m *Model) requestOrigin(t *tab) tea.Cmd {
	r, ok := m.branchRequest(t)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		v := branchOpenMsg{branchRequest: r}
		h, err := session.ReadHeader(r.path, r.id)
		if err != nil {
			v.err = err
			return v
		}
		if h.ParentSession == "" {
			v.err = fmt.Errorf("此会话没有来源记录")
			return v
		}
		v.target.Path = h.ParentSession
		if h.Fork != nil {
			v.target.ID = h.Fork.SourceID
			v.entry = h.Fork.UserEntryID
		}
		v.header, v.err = session.ReadHeader(v.target.Path, v.target.ID)
		return v
	}
}

func (m *Model) branchOpenResult(v branchOpenMsg) tea.Cmd {
	owner := m.branchOwner(v.branchRequest)
	if owner == nil {
		return nil
	}
	if v.err != nil {
		owner.notify("无法查看来源：" + v.err.Error())
		return nil
	}
	if m.reservedSession(v.target.Path) {
		owner.notify("目标会话正在修改，请稍后重试")
		return nil
	}
	jump := &branchJump{path: v.target.Path, id: v.header.ID, entry: v.entry}
	for i, t := range m.tabs {
		// Match the existing session-opening policy, including tabs originally
		// opened through a different spelling. Never start a second writer.
		if sessionPath(t.state.File) != sessionPath(v.target.Path) {
			continue
		}
		if t.mutation != nil || t.state.ID != v.header.ID {
			owner.notify("目标会话身份已变化，请重新选择")
			return nil
		}
		cmd := m.activate(i)
		jump.epoch = m.branchEpoch
		if v.entry != "" {
			t.originJump = jump
			// Do not drive a running process or navigate its tree. Its current
			// projection is enough for a read-only scroll to an existing entry.
			if !t.refreshing || len(t.state.Items) > 0 {
				m.finishOriginJump(t)
			} else {
				jump.loading = m.branchLoading(t, "等待来源历史以定位消息")
			}
		} else {
			t.notify("已打开关联会话；没有精确轮次记录时不定位消息")
		}
		return cmd
	}
	t := m.addTab(v.target.Path)
	t.state.ID, t.state.Cwd = v.header.ID, v.header.Cwd
	t.origin = v.header
	if v.entry != "" {
		t.originJump = jump
		jump.loading = m.branchLoading(t, "等待来源历史以定位消息")
	}
	focus := m.activate(len(m.tabs) - 1)
	jump.epoch = m.branchEpoch
	if v.entry == "" {
		t.notify("已打开关联会话；没有精确轮次记录时不定位消息")
	}
	return tea.Batch(focus, m.start(t))
}

func (m *Model) finishOriginJump(t *tab) {
	j := t.originJump
	if j == nil {
		return
	}
	t.originJump = nil
	if !removeBranchLoading(t, j.loading) {
		return
	}
	if t != m.current() || j.epoch != m.branchEpoch {
		return
	}
	if sessionPath(t.state.File) != sessionPath(j.path) || t.state.ID != j.id {
		t.notify("来源会话身份已变化，未定位消息")
		return
	}
	if t.selection.dragging {
		t.notify("正在选择文本；请结束选择后重新查看来源")
		return
	}
	m.document(t)
	for _, b := range t.doc.Blocks {
		if b.Role == "user" && strings.TrimSuffix(b.ID, "/0") == j.entry {
			t.follow = false
			t.offset = b.Start
			m.clampScroll(t)
			t.noteScroll()
			t.notify("已定位来源用户消息；仅滚动查看")
			return
		}
	}
	t.notify("来源消息不在当前活动分支；未改变树位置")
}
