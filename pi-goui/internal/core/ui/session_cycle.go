package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

type cyclePhase uint8

const (
	cycleIdle cyclePhase = iota
	cycleStarting
	cycleActive
	cycleClosing
)

// A tab owns at most one backend cycle. An idle tab is only a disk viewer;
// presence is advisory, and Start must still acquire the runtime's OS lock.
type sessionCycle struct {
	phase           cyclePhase
	generation      uint64
	intent          *writeIntent
	inflight        int
	checking        bool
	priming, primed bool
	checked         time.Time
	presence        rpc.SessionStatus
	probeErr        error
	probePending    bool
	probeSequence   uint64
	probed          time.Time
	preview         resumePreviewStamp
	previewPending  bool
}

type writeIntent struct {
	run   func() tea.Cmd // Invoked once, on the UI loop after authoritative load.
	draft *resumeSubmission
}

type sessionPresenceMsg struct {
	owner      *tab
	file       string
	generation uint64
	sequence   uint64
	status     rpc.SessionStatus
	err        error
	stamp      resumePreviewStamp
	stampErr   error
}

type releaseCheckMsg struct {
	id                   int
	client               *rpc.Client
	generation, revision uint64
	idle                 bool
	err                  error
	reason               string
}

type cycleClosedMsg struct {
	id         int
	client     *rpc.Client
	generation uint64
	err        error
}

type cyclePrimedMsg struct {
	id         int
	client     *rpc.Client
	generation uint64
	err        error
}

// Register the extension's idle barrier before the first operation can start
// a run. A later agent_settled alone cannot prove deferred work has finished.
func (m *Model) primeCycle(t *tab) tea.Cmd {
	if m.opts.NoSession {
		t.cycle.primed = true
		t.resume.pending = false
		return m.runCycleIntent(t)
	}
	if t.cycle.primed {
		t.resume.pending = false
		return m.runCycleIntent(t)
	}
	if t.cycle.priming || t.client == nil {
		return nil
	}
	t.cycle.priming = true
	v := cyclePrimedMsg{id: t.id, client: t.client, generation: t.cycle.generation}
	extensions := m.extensions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, v.err = extensions.CheckRelease(ctx, v.client)
		return v
	}
}

func (m *Model) cyclePrimed(v cyclePrimedMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || t.client != v.client || t.cycle.generation != v.generation || t.cycle.phase == cycleClosing {
		return nil
	}
	t.cycle.priming = false
	if v.err != nil {
		m.restoreCycleIntent(t)
		t.notify("Cannot initialize session activity: " + v.err.Error() + " · draft retained")
		return m.closeCycle(t)
	}
	t.cycle.primed = true
	t.resume.pending = false
	return m.runCycleIntent(t)
}

func (t *tab) readOnly() bool {
	return t.client == nil && t.cycle.phase != cycleStarting &&
		(t.cycle.presence.Busy || t.cycle.presence.Uncertain || t.cycle.probeErr != nil)
}

func (t *tab) ownershipStatus() string {
	if t.cycle.probeErr != nil || t.cycle.presence.Uncertain {
		reason := t.cycle.presence.Reason
		if t.cycle.probeErr != nil {
			reason = t.cycle.probeErr.Error()
		}
		if reason != "" {
			return "Read-only · session ownership unknown · " + reason
		}
		return "Read-only · session ownership unknown"
	}
	if t.cycle.presence.Busy {
		return fmt.Sprintf("working · held by another instance · PID %d · Read-only", t.cycle.presence.OwnerPID)
	}
	return "Idle"
}

func (m *Model) pollSession(t *tab, now time.Time, force bool) tea.Cmd {
	if t == nil || t.client != nil || t.cycle.phase != cycleIdle || t.mutation != nil || t.cycle.probePending ||
		(!force && !t.cycle.probed.IsZero() && now.Sub(t.cycle.probed) < time.Second) {
		return nil
	}
	t.cycle.probePending = true
	t.cycle.probeSequence++
	v := sessionPresenceMsg{owner: t, file: t.state.File, generation: t.cycle.generation, sequence: t.cycle.probeSequence}
	agentDir := m.cfg.AgentDir
	return func() tea.Msg {
		v.status, v.err = rpc.ProbeSession(agentDir, v.file)
		if v.file != "" {
			v.stamp, v.stampErr = previewStamp(v.file)
		}
		return v
	}
}

func (m *Model) sessionPresence(v sessionPresenceMsg) tea.Cmd {
	t := m.byID(v.owner.id)
	if t != v.owner || t.state.File != v.file || t.cycle.generation != v.generation || t.cycle.probeSequence != v.sequence || t.client != nil || t.cycle.phase != cycleIdle {
		return nil
	}
	t.cycle.probePending = false
	t.cycle.probed = time.Now()
	t.cycle.presence, t.cycle.probeErr = v.status, v.err
	if v.status.Busy {
		// A new writer supersedes the last local completion row, not the
		// statistics already attached to its historical assistant messages.
		t.dismissCompletedWorking()
	}
	if v.stampErr != nil && !t.emptyNewHistory(v.stampErr) {
		t.cycle.probeErr = v.stampErr
	}
	if t.readOnly() {
		t.status = t.ownershipStatus()
		// Observing another writer never opens a question or changes a draft.
		t.editor.Area.Blur()
	} else if !t.resume.failed {
		t.status = "Idle"
	}
	var cmd tea.Cmd
	if v.file != "" && v.stampErr == nil && !t.cycle.preview.matches(v.stamp) && !t.cycle.previewPending {
		cmd = m.previewSession(t)
	}
	if t == m.current() {
		return tea.Batch(cmd, m.focus(t))
	}
	return cmd
}

func (m *Model) beginCycle(t *tab, intent *writeIntent) tea.Cmd {
	if t == nil || t.readOnly() || t.mutation != nil || t.forkPending || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.client != nil && !t.dead && !t.resume.pending {
		if intent != nil && intent.run != nil {
			return intent.run()
		}
		return nil
	}
	if t.cycle.phase == cycleStarting || t.client != nil || t.cycle.intent != nil {
		t.notify("An operation is waiting for this session; draft retained")
		return nil
	}
	t.cycle.generation++
	t.cycle.phase, t.cycle.intent = cycleStarting, intent
	t.cycle.probePending, t.cycle.previewPending = false, false
	t.cycle.checking, t.cycle.inflight = false, 0
	t.cycle.primed, t.cycle.priming = false, false
	t.cycle.probeErr = nil
	t.cycle.presence = rpc.SessionStatus{}
	t.cycle.checked = time.Time{}
	t.dead, t.resume.failed, t.resume.authoritative = false, false, false
	t.resume.pending = true
	t.status = "Starting pi…"
	t.revision++
	id, generation, file := t.id, t.cycle.generation, t.state.File
	opts, agentDir, pool := m.opts, m.cfg.AgentDir, m.pool
	opts.Cwd = m.tabDirectory(t)
	return func() tea.Msg {
		client, err := pool.start(rpc.Config{Bin: opts.Bin, Cwd: opts.Cwd, AgentDir: agentDir, SessionFile: file, NoSession: opts.NoSession, Args: opts.PiArgs})
		return openedMsg{id: id, client: client, err: err, generation: generation}
	}
}

func (m *Model) cycleOpened(v openedMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || t.cycle.generation != v.generation || t.cycle.phase != cycleStarting {
		if v.client != nil {
			pool := m.pool
			return func() tea.Msg { _ = pool.closeClient(v.client); return nil }
		}
		return nil
	}
	if v.err != nil {
		t.client, t.lastEventErr = nil, nil
		m.restoreCycleIntent(t)
		t.cycle.phase = cycleIdle
		t.resume.pending = false
		t.dead = false
		var busy *rpc.SessionBusyError
		t.resume.failed = !errors.As(v.err, &busy)
		if busy != nil {
			t.cycle.presence = busy.Status
			t.cycle.probeErr = nil
			t.status = t.ownershipStatus()
		} else {
			t.status = "Cannot start pi: " + v.err.Error() + " · draft retained"
		}
		t.cycle.probed = time.Now()
		return m.focus(t)
	}
	t.client, t.lastEventErr = v.client, nil
	t.cycle.phase = cycleActive
	t.dead = false
	// Runtime chooses a stable path before startup so every later write uses
	// this exact lease even when Pi has not persisted its first record yet.
	if file := v.client.SessionFile(); file != "" {
		t.state.File = file
	}
	t.refreshing, t.refreshAgain = false, false
	t.status = "Loading current session…"
	return tea.Batch(nextEvent(t), m.refresh(t))
}

func (m *Model) restoreCycleIntent(t *tab) {
	intent := t.cycle.intent
	t.cycle.intent = nil
	t.footerHover.loading, t.footerHover.setting = false, false
	m.restoreResumeSubmission(t)
	if intent != nil && intent.draft != nil {
		m.restoreResumeDraft(t, intent.draft)
	}
}

func (m *Model) runCycleIntent(t *tab) tea.Cmd {
	if t.resume.pending || t.client == nil || t.dead || t.cycle.phase == cycleClosing {
		return nil
	}
	intent := t.cycle.intent
	t.cycle.intent = nil
	if intent != nil && intent.run != nil {
		return intent.run()
	}
	return m.flushResumeSubmission(t)
}

func (t *tab) releaseReady() bool {
	if t.client == nil || t.dead || t.cycle.phase == cycleStarting || t.cycle.phase == cycleClosing ||
		t.resume.pending || !t.resume.authoritative || !t.cycle.primed || t.cycle.priming || t.cycle.intent != nil || t.resume.submit != nil ||
		t.cycle.inflight != 0 || t.extensions.Pending() || t.refreshing || t.ownRunActive() || t.forkPending || t.mutation != nil ||
		len(t.state.Steering) != 0 || len(t.state.FollowUp) != 0 {
		return false
	}
	for _, d := range t.dialogs {
		if d.requestID != "" || d.kind == "model" || d.kind == "thinking" || d.kind == "name" {
			return false
		}
	}
	return !t.footerHover.loading && !t.footerHover.setting
}

func (m *Model) releaseReady(t *tab) bool {
	if !t.releaseReady() {
		return false
	}
	for page := &m.inspector; page != nil; page = page.parent {
		if page.tabID == t.id && extension.InspectorPending(page.content) {
			return false
		}
	}
	return true
}

func (m *Model) checkRelease(t *tab, now time.Time) tea.Cmd {
	// An explicitly non-persistent session has no history from which another
	// fresh Pi could restore context. Its local process lives until tab close.
	if m.opts.NoSession || !m.releaseReady(t) || t.cycle.checking || (!t.cycle.checked.IsZero() && now.Sub(t.cycle.checked) < time.Second) {
		return nil
	}
	t.cycle.checking = true
	v := releaseCheckMsg{id: t.id, client: t.client, generation: t.cycle.generation, revision: t.revision}
	extensions := m.extensions
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status, err := extensions.CheckRelease(ctx, v.client)
		if err != nil {
			v.err = err
			return v
		}
		if status.Known {
			v.idle = status.Idle
			if len(status.Reasons) > 0 {
				v.reason = status.Reasons[0]
			}
			return v
		}
		raw, err := v.client.Call(ctx, map[string]any{"type": "get_state"})
		if err != nil {
			v.err = err
			return v
		}
		var state struct {
			Streaming  *bool `json:"isStreaming"`
			Compacting *bool `json:"isCompacting"`
			Pending    *int  `json:"pendingMessageCount"`
		}
		if err := json.Unmarshal(raw, &state); err != nil || state.Streaming == nil || state.Compacting == nil || state.Pending == nil || *state.Pending < 0 {
			v.err = errors.New("backend did not confirm an idle session")
			return v
		}
		v.idle = !*state.Streaming && !*state.Compacting && *state.Pending == 0
		return v
	}
}

func (m *Model) releaseChecked(v releaseCheckMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || t.client != v.client || t.cycle.generation != v.generation {
		return nil
	}
	t.cycle.checking = false
	t.cycle.checked = time.Now()
	if !v.idle || v.err != nil || t.revision != v.revision || !m.releaseReady(t) {
		if t.revision == v.revision && m.releaseReady(t) {
			if v.err != nil {
				t.status = "Waiting for backend idle confirmation · " + v.err.Error()
			} else if v.reason != "" {
				t.status = "Working · " + v.reason
			}
		}
		return nil
	}
	return m.closeCycle(t)
}

func (m *Model) closeCycle(t *tab) tea.Cmd {
	if t.client == nil || t.cycle.phase == cycleClosing {
		return nil
	}
	t.cycle.phase = cycleClosing
	if !t.dead {
		t.status = "Finishing session…"
	}
	v := cycleClosedMsg{id: t.id, client: t.client, generation: t.cycle.generation}
	pool := m.pool
	return func() tea.Msg { v.err = pool.closeClient(v.client); return v }
}

func (m *Model) cycleClosed(v cycleClosedMsg) tea.Cmd {
	t := m.byID(v.id)
	if t == nil || t.client != v.client || t.cycle.generation != v.generation {
		return nil
	}
	t.client = nil
	t.cycle.phase = cycleIdle
	t.cycle.inflight, t.cycle.checking = 0, false
	t.cycle.probed = time.Time{}
	t.cycle.preview = resumePreviewStamp{}
	t.resume.pending, t.resume.authoritative = false, false
	t.refreshing, t.refreshAgain = false, false
	t.sending, t.awaitingStart, t.interruptRequested = false, false, false
	t.state.Busy, t.state.Compacting, t.working.Active = false, false, false
	t.dead = false
	t.revision++
	// Requests belong to the exited process. They cannot be answered through
	// a future backend; local dialogs and independent drafts remain intact.
	local := t.dialogs[:0]
	for _, d := range t.dialogs {
		if d.requestID == "" {
			local = append(local, d)
		}
	}
	t.dialogs = local
	if v.err != nil {
		t.cycle.probeErr = v.err
		t.status = "Read-only · backend exit unconfirmed: " + v.err.Error()
	} else {
		t.cycle.presence, t.cycle.probeErr = rpc.SessionStatus{}, nil
		t.status = "Idle"
	}
	return tea.Batch(m.pollSession(t, time.Now(), true), m.focus(t))
}

// Read-only history operations never start a backend, including double Esc.
func (m *Model) readHistory(t *tab, op string) tea.Cmd {
	if t.client != nil && !t.dead && t.cycle.phase != cycleClosing {
		return call(t, op, nil)
	}
	id, file, generation := t.id, t.state.File, t.cycle.generation
	return func() tea.Msg {
		raw, err := session.ReadFile(file)
		if file == "" {
			raw, err = json.RawMessage(`{"entries":[]}`), nil
		}
		if op == "get_fork_messages" && err == nil {
			var snapshot struct {
				Entries []struct {
					ID, Type string
					Message  struct {
						Role    string
						Content json.RawMessage
					}
				}
			}
			err = json.Unmarshal(raw, &snapshot)
			var messages []map[string]string
			for _, entry := range snapshot.Entries {
				if entry.Type != "message" || entry.Message.Role != "user" {
					continue
				}
				var text string
				if json.Unmarshal(entry.Message.Content, &text) != nil {
					var blocks []struct{ Text string }
					_ = json.Unmarshal(entry.Message.Content, &blocks)
					for _, b := range blocks {
						text += b.Text
					}
				}
				messages = append(messages, map[string]string{"entryId": entry.ID, "text": text})
			}
			raw, _ = json.Marshal(map[string]any{"messages": messages})
		}
		return replyMsg{id: id, op: op, data: raw, err: err, generation: generation, file: file}
	}
}

func (m *Model) requestCall(t *tab, op string, data map[string]any) tea.Cmd {
	if op == "get_entries" || op == "get_fork_messages" {
		return m.readHistory(t, op)
	}
	if t == nil || t.readOnly() || t.cycle.phase == cycleClosing || t.mutation != nil {
		return nil
	}
	if t.client != nil && !t.dead && !t.resume.pending {
		return call(t, op, data)
	}
	if op == "abort" || op == "clear_queue" {
		return nil
	}
	return m.beginCycle(t, &writeIntent{run: func() tea.Cmd { return call(t, op, data) }})
}

// A missing file is legitimate only before this tab's first persisted message.
func (t *tab) emptyNewHistory(err error) bool {
	return t.newSession && t.state.MessageCount == 0 && len(t.state.Items) == 0 && errors.Is(err, os.ErrNotExist)
}
