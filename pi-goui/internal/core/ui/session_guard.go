package ui

import (
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
)

const unfinishedWarning = "⚠ 会话可能仍在执行或曾被中断；继续输入前需确认，避免多个窗口同时对话。"
const uncertainWarning = "⚠ 无法确认会话已结束；继续输入前需确认，避免多个窗口同时对话。"

type sessionGuard struct {
	path                string
	warning             string
	checked             time.Time
	pending, submitting bool
	sequence            uint64
	approved, prompted  bool
}
type sessionGuardMsg struct {
	id                                       int
	path                                     string
	sequence, draftRevision, imageGeneration uint64
	draft                                    string
	submit, follow                           bool
	unfinished                               bool
	err                                      error
}

func (t *tab) ownRunActive() bool {
	return t.state.Busy || t.state.Compacting || t.sending || t.awaitingStart || t.working.active
}

// Schedule inspection from ticks/load or input attempts; never do IO in View
// or the editor key path. Submit supersedes a periodic result.
func (m *Model) inspectSession(t *tab, submit, follow bool, now time.Time) tea.Cmd {
	if t.state.File == "" || t.ownRunActive() {
		return nil
	}
	g := &t.sessionGuard
	if g.path != t.state.File {
		*g = sessionGuard{path: t.state.File, sequence: g.sequence + 1}
	}
	if g.submitting || (!submit && (g.pending || (!g.checked.IsZero() && now.Sub(g.checked) < 2*time.Second))) {
		return nil
	}
	g.sequence++
	g.pending = true
	g.submitting = submit
	msg := sessionGuardMsg{id: t.id, path: t.state.File, sequence: g.sequence, draftRevision: t.draftRevision, imageGeneration: t.imageGeneration, draft: t.editor.Value(), submit: submit, follow: follow}
	// Pi allocates a path before persisting a new session's first message.
	// Only that empty, locally created session may legitimately have no file.
	newEmpty := t.newSession && t.state.MessageCount == 0 && len(t.state.Items) == 0
	return func() tea.Msg {
		msg.unfinished, msg.err = session.UnfinishedFile(msg.path)
		if newEmpty && errors.Is(msg.err, os.ErrNotExist) {
			msg.err = nil
		}
		return msg
	}
}
func (m *Model) sessionGuardResult(v sessionGuardMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || t.state.File != v.path || t.sessionGuard.sequence != v.sequence {
		return nil
	}
	g := &t.sessionGuard
	g.pending, g.submitting = false, false
	g.checked = time.Now()
	previous := g.warning
	g.warning = ""
	if v.unfinished {
		g.warning = unfinishedWarning
	}
	if v.err != nil {
		g.warning = uncertainWarning
	}
	if g.warning != previous {
		g.approved, g.prompted = false, false
	}
	if !t.ownRunActive() && g.warning != "" && !g.approved {
		if !g.prompted {
			return m.sessionGateDialog(t)
		}
		return nil
	}
	if !v.submit {
		return nil
	}
	// Never send a changed draft or images on behalf of an earlier keypress.
	if !v.matches(t) {
		return nil
	}
	return m.submitChecked(t, v.follow, true)
}
func (v sessionGuardMsg) matches(t *tab) bool {
	return t.state.File == v.path && t.draftRevision == v.draftRevision && t.imageGeneration == v.imageGeneration && t.editor.Value() == v.draft
}
func (m *Model) confirmSessionSend(t *tab, d *dialog, value string) tea.Cmd {
	if value != "continue" || d.sessionSend == nil {
		return nil
	}
	v := d.sessionSend
	if t.state.File == v.path && t.sessionGuard.path == v.path && t.sessionGuard.warning == d.message {
		t.sessionGuard.approved = true
	}
	return nil
}

// gateSession is used before any user-driven draft mutation. Initial inspection
// is asynchronous; the triggering input is intentionally not replayed.
func (m *Model) gateSession(t *tab) (bool, tea.Cmd) {
	if t.state.File == "" || t.ownRunActive() {
		return false, nil
	}
	g := &t.sessionGuard
	if g.path != t.state.File || g.checked.IsZero() {
		return true, m.inspectSession(t, false, false, time.Now())
	}
	if g.warning != "" && !g.approved {
		return true, m.sessionGateDialog(t)
	}
	return false, nil
}

func (m *Model) sessionGateDialog(t *tab) tea.Cmd {
	for _, d := range t.dialogs {
		if d.kind == "session-send-confirm" {
			return nil
		}
	}
	g := &t.sessionGuard
	g.prompted = true
	d := m.newDialog("session-send-confirm", "确认继续未结束会话", "")
	d.message = g.warning
	d.options = []option{{Label: "确认，继续此会话", Value: "continue"}}
	d.sessionSend = &sessionGuardMsg{path: t.state.File}
	t.dialogs = append([]*dialog{d}, t.dialogs...)
	return m.focus(t)
}
