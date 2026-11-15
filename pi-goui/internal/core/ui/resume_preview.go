package ui

import (
	"fmt"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

// A disk preview is only a local projection. Neither it nor an opened transport
// authorizes sending: the first current RPC snapshot must commit first.
type resumeConnection struct {
	pending, preview, authoritative, failed bool
	submit                                  *resumeSubmission
}

type resumeSubmission struct {
	original string
	images   []map[string]any
	followUp bool
	checking bool
}

type resumeSubmitReadyMsg struct {
	owner   *tab
	client  *rpc.Client
	file    string
	pending *resumeSubmission
	err     error
}

type resumePreviewMsg struct {
	owner      *tab
	client     *rpc.Client
	file       string
	revision   uint64
	generation uint64
	header     session.Header
	prepared   *session.PreparedSnapshot
	stamp      resumePreviewStamp
	err        error
}

func (m *Model) previewSession(t *tab) tea.Cmd {
	if t.state.File == "" || t.client != nil || t.cycle.previewPending || t.cycle.phase == cycleClosing {
		return nil
	}
	owner, client, file, revision := t, t.client, resumePreviewPath(t.state.File), t.revision
	t.cycle.previewPending = true
	v := resumePreviewMsg{owner: owner, client: client, file: file, revision: revision, generation: t.cycle.generation}
	p := &m.resumePreload
	if cached := p.cached; cached != nil && cached.file == file && cached.stamp.current(file) {
		v.header, v.prepared, v.err, v.stamp = cached.header, cached.prepared, cached.err, cached.stamp
		return func() tea.Msg { return v }
	}
	if p.job != nil && p.job.file == file && p.job.stamp.current(file) {
		for _, waiter := range p.waiters {
			if waiter.owner == owner && waiter.revision == revision && waiter.file == file {
				return nil
			}
		}
		p.waiters = append(p.waiters, v)
		return nil
	}
	preparation := t.state.SnapshotPreparation()
	return func() tea.Msg {
		v.stamp, v.err = previewStamp(file)
		if v.err == nil {
			v.header, v.prepared, v.err = preparation.PrepareFile(file)
		}
		return v
	}
}

func (m *Model) receiveResumePreview(v resumePreviewMsg) tea.Cmd {
	t := m.byID(v.owner.id)
	if t != v.owner || t.cycle.generation != v.generation || resumePreviewPath(t.state.File) != v.file || t.client != v.client {
		return nil
	}
	t.cycle.previewPending = false
	if t.resume.authoritative || t.revision != v.revision {
		return nil
	}
	if v.err == nil && v.stamp.info != nil && !v.stamp.current(v.file) {
		return nil // The next presence poll retries a concurrently appended file.
	}
	if v.err != nil {
		if !t.resume.failed && !t.readOnly() && !t.emptyNewHistory(v.err) {
			t.status = "暂时无法读取会话历史：" + v.err.Error()
		}
		return nil
	}
	t.state.ApplySnapshot(v.prepared)
	t.state.Busy, t.state.Compacting = false, false
	t.state.Steering, t.state.FollowUp = nil, nil
	t.cycle.preview = v.stamp
	t.revision++
	t.origin = v.header
	t.resume.preview = true
	if !t.resume.failed {
		t.status = t.ownershipStatus()
	}
	// Do not touch the editor, attachments, draft revision or scroll position.
	t.invalidateDocument()
	// Treat the first layout like streaming work: keep typing responsive while
	// Markdown/tool rendering runs, rather than doing it in this update.
	t.streamDirty = true
	return m.scheduleDocument(t)
}

func (m *Model) deferResumeSubmission(t *tab, followUp bool) tea.Cmd {
	if t.resume.submit != nil {
		t.notify("上一条输入等待连接；当前草稿已保留")
		return nil
	}
	t.resume.submit = &resumeSubmission{original: t.editor.Value(), images: append([]map[string]any(nil), t.images...), followUp: followUp}
	t.setValue("")
	t.clearImages()
	t.historyIndex = -1
	m.completionIndex = 0
	m.completionHidden = false
	t.dismissCompletedWorking()
	m.resizeEditor(t)
	return nil
}

func (m *Model) flushResumeSubmission(t *tab) tea.Cmd {
	pending := t.resume.submit
	if pending == nil || t.resume.pending || t.resume.failed || t.dead || t.client == nil {
		return nil
	}
	if pending.checking {
		return nil
	}
	// A slow connect may outlive the inspection performed at Enter. Apply the
	// same idle-history guard again without substituting a newer editor draft.
	if !t.ownRunActive() && t.state.File != "" && !(t.newSession && t.state.MessageCount == 0 && len(t.state.Items) == 0) {
		pending.checking = true
		v := resumeSubmitReadyMsg{owner: t, client: t.client, file: t.state.File, pending: pending}
		return func() tea.Msg {
			_, v.err = session.UnfinishedFile(v.file)
			return v
		}
	}
	return m.sendResumeSubmission(t, pending)
}

func (m *Model) sendResumeSubmission(t *tab, pending *resumeSubmission) tea.Cmd {
	// Clear before returning the command: duplicate snapshots cannot submit twice.
	t.resume.submit = nil
	return m.sendPrompt(t, pending.original, pending.images, pending.followUp, false)
}

func (m *Model) resumeSubmissionReady(v resumeSubmitReadyMsg) tea.Cmd {
	t := m.byID(v.owner.id)
	if t != v.owner || t.resume.submit != v.pending {
		return nil
	}
	if t.client != v.client || t.state.File != v.file || t.dead || t.resume.failed {
		m.restoreResumeSubmission(t)
		return nil
	}
	if v.err != nil && !t.ownRunActive() && !t.sessionGuard.approved {
		m.restoreResumeSubmission(t)
		t.sessionGuard.path = t.state.File
		t.sessionGuard.warning = uncertainWarning
		t.sessionGuard.checked = time.Now()
		return m.sessionGateDialog(t)
	}
	return m.sendResumeSubmission(t, v.pending)
}

func (m *Model) restoreResumeSubmission(t *tab) {
	pending := t.resume.submit
	if pending == nil {
		return
	}
	t.resume.submit = nil
	m.restoreResumeDraft(t, pending)
}

func (m *Model) restoreResumeDraft(t *tab, pending *resumeSubmission) {
	later := t.editor.Value()
	if len(pending.images) > 0 {
		later = imageReference.ReplaceAllStringFunc(later, func(marker string) string {
			n, _ := strconv.Atoi(marker[6 : len(marker)-1])
			if n > len(t.images) {
				return marker
			}
			return fmt.Sprintf("[image%d]", n+len(pending.images))
		})
	}
	text := pending.original
	if later != "" {
		text += "\n\n" + later
	}
	t.images = append(append([]map[string]any(nil), pending.images...), t.images...)
	t.setValue(text)
	m.resizeEditor(t)
}

func (m *Model) failResume(t *tab, err error) {
	t.resume.pending = false
	t.resume.failed = true
	m.restoreCycleIntent(t)
	t.status = "连接会话失败：" + err.Error() + "；历史和未发送内容已保留"
}

func (t *tab) resumingHistory() bool {
	return t.state.File != "" && !t.resume.authoritative && (t.resume.pending || t.resume.preview || t.resume.failed)
}
