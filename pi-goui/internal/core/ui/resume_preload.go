package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

// Speculation owns one result, one running read and one replacement candidate,
// never a queue of mouse movements. Large files still use the normal cold path.
const resumePreloadLimit = 64 << 20

type resumePreviewStamp struct {
	canonical string
	info      os.FileInfo
}

// Preserve the selected spelling for Lstat: resolving a final symlink before
// validation would silently turn a rejected file into an accepted target.
func resumePreviewPath(file string) string {
	if file == "" {
		return ""
	}
	if absolute, err := filepath.Abs(file); err == nil {
		return filepath.Clean(absolute)
	}
	return filepath.Clean(file)
}

func previewStamp(file string) (resumePreviewStamp, error) {
	info, err := os.Lstat(file)
	if err != nil {
		return resumePreviewStamp{}, err
	}
	if !info.Mode().IsRegular() {
		return resumePreviewStamp{}, fmt.Errorf("history is not a regular file")
	}
	canonical, err := filepath.EvalSymlinks(file)
	if err != nil {
		return resumePreviewStamp{}, err
	}
	return resumePreviewStamp{canonical: canonical, info: info}, nil
}
func (s resumePreviewStamp) matches(other resumePreviewStamp) bool {
	return s.info != nil && other.info != nil && s.canonical == other.canonical && os.SameFile(s.info, other.info) && s.info.Size() == other.info.Size() && s.info.ModTime().Equal(other.info.ModTime()) && s.info.Mode() == other.info.Mode()
}
func (s resumePreviewStamp) current(file string) bool {
	now, err := previewStamp(file)
	return err == nil && s.matches(now)
}

type resumePreloadEntry struct {
	file     string
	stamp    resumePreviewStamp
	header   session.Header
	prepared *session.PreparedSnapshot
	err      error
}
type resumePreloadJob struct {
	file  string
	stamp resumePreviewStamp
}
type resumePreloadMsg struct {
	job   *resumePreloadJob
	entry resumePreloadEntry
}
type resumePreloadState struct {
	cached  *resumePreloadEntry
	job     *resumePreloadJob
	wanted  string
	waiters []resumePreviewMsg
}

func (m *Model) requestResumePreload(file string) tea.Cmd {
	p := &m.resumePreload
	file = resumePreviewPath(file)
	p.wanted = file
	if file == "" || m.opts.NoSession {
		return nil
	}
	stamp, err := previewStamp(file)
	if err != nil || stamp.info.Size() > resumePreloadLimit {
		return nil
	}
	if p.cached != nil && p.cached.file == file && p.cached.stamp.matches(stamp) {
		return nil
	}
	if p.job != nil {
		return nil
	}
	// Drop the old entry before allocating its replacement.
	p.cached = nil
	job := &resumePreloadJob{file: file, stamp: stamp}
	p.job = job
	return func() tea.Msg {
		var empty session.State
		entry := resumePreloadEntry{file: file, stamp: stamp}
		entry.header, entry.prepared, entry.err = empty.SnapshotPreparation().PrepareFile(file)
		if entry.err == nil && !stamp.current(file) {
			entry.prepared = nil
			entry.err = fmt.Errorf("history changed while preloading")
		}
		return resumePreloadMsg{job: job, entry: entry}
	}
}

func (m *Model) receiveResumePreload(v resumePreloadMsg) tea.Cmd {
	p := &m.resumePreload
	if p.job != v.job {
		return nil
	}
	waiters := p.waiters
	p.job, p.waiters = nil, nil
	valid := v.entry.stamp.current(v.entry.file)
	if valid && p.wanted == v.entry.file {
		p.cached = &v.entry
	}
	var cmds []tea.Cmd
	for _, waiter := range waiters {
		if t := m.byID(waiter.owner.id); t != waiter.owner || t.resume.authoritative || t.revision != waiter.revision || resumePreviewPath(t.state.File) != waiter.file || (waiter.client != nil && waiter.client != t.client) {
			continue
		}
		if !valid {
			cmds = append(cmds, m.previewSession(waiter.owner))
			continue
		}
		waiter.header, waiter.prepared, waiter.err = v.entry.header, v.entry.prepared, v.entry.err
		waiter.stamp = v.entry.stamp
		cmds = append(cmds, func() tea.Msg { return waiter })
	}
	cmds = append(cmds, m.requestResumePreload(p.wanted))
	return tea.Batch(cmds...)
}

// Resolve the same filtered/threaded row as selection and hit testing. Merely
// moving over a row does not acquire a backend or change the active tab.
func (m *Model) resumePreloadCandidate() string {
	t := m.current()
	if t == nil || m.opts.NoSession {
		return ""
	}
	if len(t.dialogs) > 0 {
		d := t.dialogs[0]
		if d.kind != "session" {
			return ""
		}
		options := m.filtered(d)
		if d.selected < 0 || d.selected >= len(options) || strings.HasPrefix(options[d.selected].Value, "@") {
			return ""
		}
		return options[d.selected].Value
	}
	if m.inspectorOpen() || m.extensionPopupVisible() || m.sidebarWidth() == 0 || m.sidebarHistory.cwd != m.tabDirectory(t) {
		return ""
	}
	layout := m.sidebarLayout()
	index := layout.HistoryOffset
	h := m.sidebarHistory
	if h.pointerKnown && h.pointerX >= m.mainWidth()+2 && h.pointerX < m.width-2 {
		row := h.pointerY - layout.HistoryStart
		if row >= 0 && row < layout.HistoryCount*render.SidebarHistoryRows {
			index += row / render.SidebarHistoryRows
		}
	}
	if layout.HistoryCount == 0 {
		return ""
	}
	file := m.sidebarHistoryAt(index).Path
	for _, open := range m.tabs {
		if !open.dead && sameSessionPath(open.state.File, file) {
			return ""
		}
	}
	return file
}

func (m *Model) ensureResumePreload() tea.Cmd {
	return m.requestResumePreload(m.resumePreloadCandidate())
}
