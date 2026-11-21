package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

// A dialog captures a stable target, never a row index or whichever tab happens
// to be active when its asynchronous work finishes.
type sessionAction struct {
	kind, key, path, sessionID, value, name string
	originID, targetID                      int
	client                                  *rpc.Client
	priorDead, disconnected                 bool
}
type sessionActionMsg struct {
	action *sessionAction
	err    error
	closed bool
}

func (m *Model) sessionSelectorHelp() string {
	help := "New: /new"
	for _, item := range []struct{ action, label string }{{"app.session.rename", "rename"}, {"app.session.delete", "delete"}} {
		if len(m.cfg.Keys.Keys(item.action)) > 0 {
			help += " · " + m.compactKey(item.action, "") + " " + item.label
		}
	}
	return help
}
func sameSessionFile(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if sessionPath(left) == sessionPath(right) {
		return true
	}
	a, errA := os.Stat(left)
	b, errB := os.Stat(right)
	return errA == nil && errB == nil && os.SameFile(a, b)
}
func (m *Model) reservedSession(path string) bool {
	if path == "" {
		return false
	}
	for _, action := range m.sessionActions {
		if sameSessionFile(path, action.path) {
			return true
		}
	}
	return false
}

// Reuse the selector's identity checks and confirmation without opening a
// session list. The target is captured now, not when deletion finishes.
func (m *Model) requestDeleteSession(t *tab) tea.Cmd {
	selector := m.newDialog("session", "", "")
	selector.options = []option{{
		Label: m.sessionLabel(t), Value: fmt.Sprintf("@%d", t.id), Data: summaryOf(t),
	}}
	cmd := m.requestSessionAction(t, selector, "delete")
	if selector.message != "" {
		t.notify(selector.message)
	}
	return cmd
}

func (m *Model) requestRecentDelete(origin *tab, s session.Summary) tea.Cmd {
	value := s.Path
	for _, target := range m.tabs {
		if target.state.ID == s.ID && sameSessionFile(target.state.File, s.Path) {
			value = fmt.Sprintf("@%d", target.id)
			break
		}
	}
	selector := m.newDialog("session", "", "")
	label := s.Name
	if label == "" {
		label = s.ID
	}
	selector.options = []option{{Label: label, Value: value, Data: s}}
	cmd := m.requestSessionAction(origin, selector, "delete")
	if selector.message != "" {
		origin.notify(selector.message)
	}
	return cmd
}

func (m *Model) requestSessionAction(origin *tab, selector *dialog, kind string) tea.Cmd {
	options := m.filtered(selector)
	if len(options) == 0 {
		return nil
	}
	selected := options[min(max(0, selector.selected), len(options)-1)]
	summary, ok := selected.Data.(session.Summary)
	if !ok {
		selector.message = "Session information is unavailable."
		return nil
	}
	a := &sessionAction{kind: kind, originID: origin.id, path: summary.Path, sessionID: summary.ID, value: selected.Value}
	name := summary.Name
	if strings.HasPrefix(selected.Value, "@") {
		_, _ = fmt.Sscanf(selected.Value, "@%d", &a.targetID)
		target := m.byID(a.targetID)
		if target == nil {
			selector.message = "This session is no longer open."
			return nil
		}
		if target.mutation != nil {
			selector.message = "A session operation is already in progress."
			return nil
		}
		if target.cycle.phase == cycleStarting || target.cycle.phase == cycleClosing {
			selector.message = "Wait until this session has finished starting."
			return nil
		}
		a.path, a.sessionID = target.state.File, target.state.ID
		name = target.state.Name
	} else {
		// A saved row may have been opened after the selector was created.
		for _, target := range m.tabs {
			if sameSessionFile(target.state.File, a.path) {
				selector.message = "Session was opened; reopen the selector before modifying it."
				return nil
			}
		}
	}
	if a.path != "" {
		a.key = sessionPath(a.path)
	} else {
		a.key = fmt.Sprintf("@%d", a.targetID)
	}
	for _, target := range m.tabs {
		if target.id != a.targetID && sameSessionFile(target.state.File, a.path) {
			selector.message = "Another open tab aliases this history; close the duplicate first."
			return nil
		}
	}
	if m.sessionActions[a.key] != nil || m.reservedSession(a.path) {
		selector.message = "A session operation is already in progress."
		return nil
	}
	title, message := "Rename session: "+selected.Label, "Enter saves the name. Esc cancels. An empty name clears the title."
	if kind == "delete" {
		title = "Delete session: " + selected.Label + "?"
		message = "Permanent deletion. Enter confirms; Esc cancels."
		if a.targetID != 0 {
			message = "Permanent deletion; running work, draft and attachments will be lost."
		}
		if a.path != "" {
			message += "\n" + a.path
		}
	}
	d := m.newDialog("session-"+kind, title, message)
	d.action = a
	if kind == "rename" {
		d.editor.SetValue(name)
	} else {
		d.options = []option{{Label: "Delete session", Value: "yes"}, {Label: "Cancel", Value: "no"}}
	}
	origin.dialogs = append([]*dialog{d}, origin.dialogs...)
	return m.focus(origin)
}

func (m *Model) beginSessionAction(origin *tab, d *dialog, value string) tea.Cmd {
	a := d.action
	if a == nil {
		return nil
	}
	if a.kind == "delete" && value != "yes" {
		origin.dialogs = origin.dialogs[1:]
		return m.focus(origin)
	}
	if m.sessionActions[a.key] != nil || m.reservedSession(a.path) {
		d.message = "A session operation is already in progress."
		return nil
	}
	for _, tab := range m.tabs {
		if tab.id != a.targetID && sameSessionFile(tab.state.File, a.path) {
			d.message = "Session is now open in another tab; cancel and select it again."
			return nil
		}
	}
	target := m.byID(a.targetID)
	if a.targetID != 0 {
		if target == nil || target.mutation != nil || target.state.ID != a.sessionID || target.state.File != a.path {
			d.message = "Selected session changed; cancel and select it again."
			return nil
		}
		if target.cycle.phase == cycleStarting || target.cycle.phase == cycleClosing {
			d.message = "Wait until the session finishes starting."
			return nil
		}
		if target.readOnly() {
			d.message = target.ownershipStatus()
			return nil
		}
		a.client = target.client
		a.priorDead = target.dead
		target.mutation = a
		if a.kind == "delete" {
			if target.client != nil {
				target.cycle.phase = cycleClosing
			}
		}
	}
	if m.sessionActions == nil {
		m.sessionActions = map[string]*sessionAction{}
	}
	m.sessionActions[a.key] = a
	a.name = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
	origin.dialogs = origin.dialogs[1:]
	agentDir := m.cfg.AgentDir
	return func() tea.Msg {
		result := sessionActionMsg{action: a}
		if a.client != nil && a.kind == "rename" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			raw, err := a.client.Call(ctx, map[string]any{"type": "get_state"})
			if err != nil {
				result.err = err
				return result
			}
			var current struct {
				ID   string `json:"sessionId"`
				File string `json:"sessionFile"`
			}
			if err = json.Unmarshal(raw, &current); err != nil {
				result.err = err
				return result
			}
			if current.ID != a.sessionID || sessionPath(current.File) != sessionPath(a.path) {
				result.err = errors.New("RPC session changed since selection")
				return result
			}
			_, result.err = a.client.Call(ctx, map[string]any{"type": "set_session_name", "name": a.name})
			return result
		}
		if a.client != nil {
			result.err = a.client.Close()
			result.closed = true
			if result.err != nil {
				return result
			}
		}
		if a.path == "" {
			if a.kind == "rename" {
				result.err = errors.New("save a message before renaming this session")
			}
			return result
		}
		// Even a successful presence poll cannot authorize a short write. Take
		// the same OS lease as Start, then inspect the current native file.
		lease, err := rpc.AcquireSession(agentDir, a.path)
		if err != nil {
			result.err = err
			return result
		}
		defer lease.Close()
		file, err := session.InspectMutationFile(a.path, a.sessionID)
		if os.IsNotExist(err) && a.kind == "delete" && a.client != nil {
			return result
		}
		if err != nil {
			result.err = err
			return result
		}
		if a.kind == "rename" {
			result.err = file.Rename(a.name)
		} else {
			result.err = file.Delete()
		}
		return result
	}
}

func (m *Model) finishSessionAction(v sessionActionMsg) tea.Cmd {
	a := v.action
	if a == nil || m.sessionActions[a.key] != a {
		return nil
	}
	delete(m.sessionActions, a.key)
	target := m.byID(a.targetID)
	if target != nil && target.mutation == a {
		target.mutation = nil
		if v.closed {
			target.cycle.generation++
			target.cycle.phase = cycleIdle
			target.cycle.inflight = 0
			target.client = nil
			target.dead = false
			target.state.Busy, target.state.Compacting, target.working.Active = false, false, false
			target.sending, target.awaitingStart, target.interruptRequested = false, false, false
			target.refreshing, target.refreshAgain = false, false
			target.resume.pending, target.resume.authoritative = false, false
		}
	}
	origin := m.byID(a.originID)
	if v.err != nil {
		message := "Session " + a.kind + " failed: " + v.err.Error()
		if target != nil && v.closed {
			target.status = message + "; history and draft retained."
			m.patchSessionSelectors(a, target, false)
		}
		if origin == nil {
			origin = m.current()
		}
		if origin != nil {
			if origin != target || !v.closed {
				origin.status = message
			}
			for _, d := range origin.dialogs {
				if d.kind == "session" {
					d.message = message
				}
			}
		}

		return nil
	}
	m.patchHistoryAfterAction(a)
	if a.kind == "rename" {
		if target != nil {
			target.state.Name = a.name
			target.invalidateDocument()
			target.revision++
		}
		m.patchSessionSelectors(a, target, false)
		return nil
	}
	var chooser *dialog
	if target != nil && origin == target {
		for _, d := range target.dialogs {
			if d.kind == "session" {
				chooser = d
				break
			}
		}
	}
	m.patchSessionSelectors(a, target, true)
	if target == nil {
		return m.requestSidebarHistory(time.Now())
	}
	// The command has already awaited Close, so removal never races its writer.
	var fresh *tab
	if len(m.tabs) == 1 {
		fresh = m.addTab("")
	}
	closed := m.closeSession(target)
	var focus tea.Cmd
	if chooser != nil {
		focus = m.restoreSessionSelector(chooser)
	}
	if fresh != nil {
		return tea.Batch(closed, focus, m.start(fresh), m.requestSidebarHistory(time.Now()))
	}
	return tea.Batch(closed, focus, m.requestSidebarHistory(time.Now()))
}

// Both the sidebar and open selectors consume these scans. A scan started
// before a durable rename or deletion must not put the old row back.
func (m *Model) patchHistoryAfterAction(a *sessionAction) {
	h := &m.sidebarHistory
	h.generation++
	h.pending = false
	h.checked = time.Time{}
	patch := func(list []session.Summary) []session.Summary {
		if list == nil {
			return nil
		}
		out := make([]session.Summary, 0, len(list))
		for _, saved := range list {
			if sameSessionPath(saved.Path, a.path) {
				if a.kind == "delete" {
					continue
				}
				saved.Name = a.name
			}
			out = append(out, saved)
		}
		return out
	}
	h.sessions = patch(h.sessions)
	for cwd, cached := range h.cache {
		cached.sessions = patch(cached.sessions)
		cached.checked = time.Time{}
		h.cache[cwd] = cached
	}
}

func (m *Model) restoreSessionSelector(previous *dialog) tea.Cmd {
	owner := m.current()
	if owner == nil {
		return nil
	}
	var saved []session.Summary
	for _, o := range previous.options {
		if !strings.HasPrefix(o.Value, "@") {
			if s, ok := o.Data.(session.Summary); ok {
				saved = append(saved, s)
			}
		}
	}
	queued := owner.dialogs
	m.sessionDialog(owner, saved, previous.hideCurrent)
	d := owner.dialogs[len(owner.dialogs)-1]
	d.editor.SetValue(previous.editor.Value())
	d.namedOnly, d.showPath, d.reverseSort = previous.namedOnly, previous.showPath, previous.reverseSort
	d.selected = min(previous.selected, max(0, len(m.filtered(d))-1))
	owner.dialogs = append([]*dialog{d}, queued...)
	return m.focus(owner)
}

func (m *Model) patchSessionSelectors(a *sessionAction, target *tab, remove bool) {
	for _, owner := range m.tabs {
		for _, d := range owner.dialogs {
			if d.kind != "session" {
				continue
			}
			out := make([]option, 0, len(d.options))
			for _, o := range d.options {
				summary, ok := o.Data.(session.Summary)
				matches := o.Value == a.value || ok && a.path != "" && sessionPath(summary.Path) == a.key
				if !matches {
					out = append(out, o)
					continue
				}
				if remove {
					continue
				}
				summary.Name = a.name
				label := a.name
				if target != nil {
					summary.Name = target.state.Name
					summary.ID = target.state.ID
					summary.Path = target.state.File
					o.Value = fmt.Sprintf("@%d", target.id)
					label = m.sessionLabel(target)
					status := "open"
					if target == owner {
						status = "current"
					} else if target.state.Busy {
						status = "working"
					}
					label += "  [" + status + "]"
				} else if label == "" {
					label = summary.FirstMessage
					if label == "" {
						label = summary.ID
					}
				}
				o.Label = label
				o.Data = summary
				out = append(out, o)
			}
			d.options = out
			d.selected = min(d.selected, max(0, len(m.filtered(d))-1))
		}
	}
}
