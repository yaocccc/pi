package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
	"pi-goui/internal/updates"
)

type updateState struct {
	reader   *updateReader
	notice   string
	notes    updates.ReleaseNotes
	notesAck bool
}

type releaseNotesMsg struct{ notes updates.ReleaseNotes }

type updateCheckMsg struct {
	report updates.Report
	err    error
}

// Cancel and reap an active check at shutdown, including commands started late.
type updateReader struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	check  func(context.Context, updates.Config) (updates.Report, error)
}

func newUpdateReader() *updateReader {
	ctx, cancel := context.WithCancel(context.Background())
	return &updateReader{ctx: ctx, cancel: cancel, check: updates.Check}
}

func (r *updateReader) close() {
	r.cancel()
	r.mu.Lock()
	r.mu.Unlock()
}

func (r *updateReader) command(cfg updates.Config) tea.Cmd {
	return func() tea.Msg {
		r.mu.Lock()
		defer r.mu.Unlock()
		if err := r.ctx.Err(); err != nil {
			return updateCheckMsg{err: err}
		}
		report, err := r.check(r.ctx, cfg)
		return updateCheckMsg{report: report, err: err}
	}
}

func (m *Model) updateConfig() updates.Config {
	bin := m.opts.Bin
	if bin == "" {
		bin = os.Getenv("PI_BIN")
	}
	cfg := updates.Config{PiBin: bin, AgentDir: m.cfg.AgentDir}
	for _, arg := range m.opts.PiArgs {
		if arg == "--offline" {
			cfg.Offline = true
		}
	}
	return cfg
}

func (m *Model) receiveUpdateCheck(msg updateCheckMsg) {
	if msg.err != nil || !msg.report.HasUpdates() {
		return
	}
	var items []string
	if msg.report.LatestVersion != "" {
		items = append(items, "Pi "+msg.report.LatestVersion)
	}
	if n := len(msg.report.Packages); n > 0 {
		items = append(items, fmt.Sprintf("%d extension package(s)", n))
	}
	m.updates.notice = "↑ Updates available: " + strings.Join(items, " · ")
	for _, t := range m.tabs {
		t.dirty = true
	}
}

const welcomeText = "\n  A quiet place to work.\n\n  Ask pi a question, or use / to explore commands.\n  Create a second session with /new; both can run in parallel."

func (m *Model) releaseNotesCommand() tea.Cmd {
	cfg := m.updateConfig()
	return func() tea.Msg { return releaseNotesMsg{updates.LoadReleaseNotes(cfg)} }
}

func (m *Model) receiveReleaseNotes(msg releaseNotesMsg) {
	m.updates.notes = msg.notes
	for _, t := range m.tabs {
		t.dirty = true
		if t.newSession && len(t.state.Items) == 0 && msg.notes.Markdown != "" {
			t.follow = false
			t.offset = 0
		}
	}
}

func (m *Model) showStartupInfo(t *tab) bool {
	return t.newSession && len(t.state.Items) == 0 && (m.updates.notice != "" || m.updates.notes.Markdown != "")
}

func (m *Model) startupDocument(t *tab) render.Document {
	text := m.paint(welcomeText, m.cfg.Theme.Muted)
	if m.updates.notice != "" {
		// Wrap with the same transcript width; no footer/banner or popup.
		notice := ansi.Wrap(m.updates.notice+"\nRun `pi update` to update.", max(1, m.mainWidth()-4), "")
		text += "\n\n" + m.paint("  "+strings.ReplaceAll(notice, "\n", "\n  "), m.cfg.Theme.Muted)
	}
	if n := m.updates.notes; n.Markdown != "" {
		item := session.Item{ID: "pi-release-notes", Role: "assistant", Text: n.Markdown}
		notes := t.renderCache.Transcript([]session.Item{item}, m.cfg.Theme, render.Options{Width: m.mainWidth(), AssistantTitle: "Pi " + n.Version + " — What's new"})
		text += "\n\n" + notes.Text
	}
	// Presentation only: never append notes to Pi history or model messages.
	return render.Document{Text: text}
}

func (m *Model) markReleaseNotesDisplayed() tea.Cmd {
	t := m.current()
	if m.updates.notesAck || m.updates.notes.Markdown == "" || t == nil || !t.newSession || len(t.state.Items) != 0 || len(t.dialogs) != 0 || t.doc.Text == "" || m.height < 8 || t.selection.dragging {
		return nil
	}
	m.updates.notesAck = true
	notes := m.updates.notes
	return func() tea.Msg { _ = notes.MarkSeen(); return nil }
}
