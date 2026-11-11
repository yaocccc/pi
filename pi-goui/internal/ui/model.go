// Package ui owns all presentation and per-session interaction state.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/codex"
	"pi-goui/internal/config"
	"pi-goui/internal/input"
	"pi-goui/internal/render"
	"pi-goui/internal/rpc"
	"pi-goui/internal/session"
)

type Options struct {
	Cwd, Bin, SessionFile string
	NoSession             bool
	PiArgs                []string
}
type tab struct {
	footerHover                                                                    footerHoverState
	newSession                                                                     bool
	mutation                                                                       *sessionAction
	awaitingStart, interruptRequested                                              bool
	id                                                                             int
	client                                                                         *rpc.Client
	state                                                                          session.State
	editor                                                                         input.Editor
	doc                                                                            render.Document
	renderCache                                                                    render.TranscriptCache
	dirty, follow, expanded, showThinking, dead, sending, refreshing, refreshAgain bool
	offset, unread, historyIndex                                                   int
	history                                                                        []string
	historyDraft, status                                                           string
	folds                                                                          map[string]bool
	dialogs                                                                        []*dialog
	commands                                                                       []command
	statuses                                                                       map[string]string
	widgets                                                                        map[string][]string
	images                                                                         []map[string]any
	imagePending                                                                   int
	imageGeneration                                                                uint64
	// revision protects asynchronous snapshots; draftRevision protects failed submissions.
	revision, draftRevision uint64
	title                   string
	selection               transcriptSelection
	scrollbarUntil          time.Time
	working                 workingState
	workingStart            int
	assistantStats          map[string]string
	toast                   toastState
}
type command struct{ Name, Description string }
type option struct {
	Label, Detail, Value string
	Data                 any
}
type dialog struct {
	themeBefore                      *config.Theme
	cwd                              *cwdCompletion
	questionnaire                    *questionnaire
	workerSettings                   *workerSettingsDialog
	hideCurrent                      bool
	action                           *sessionAction
	kind, title, message, requestID  string
	options                          []option
	selected                         int
	editor                           input.Editor
	deadline                         time.Time
	namedOnly, showPath, reverseSort bool
}
type Model struct {
	pointerShape                   string
	sidebarHistory                 sidebarHistoryState
	updates                        updateState
	sessionActions                 map[string]*sessionAction
	files                          fileCompletionState
	fileReader                     fileReader
	opts                           Options
	cfg                            config.Config
	tabs                           []*tab
	active, nextID, width, height  int
	pool                           *clientPool
	git                            gitReader
	quota                          *codex.Client
	quotaSnapshot                  codex.Snapshot
	quotaKey                       string
	quotaGeneration                uint64
	quotaPending                   bool
	quotaChecked                   time.Time
	gitInfo                        *render.GitStatus
	gitCwd                         string
	gitPending                     bool
	gitChecked                     time.Time
	sidebarOffset, completionIndex int
	completionHidden               bool
	search, searching              string
	clipboardTarget                *tab
	imagePreviews                  imagePreviewStore
	lastEscape, lastClear          time.Time
	frame                          int
	codexFastLabel                 string
	codexFastChecked               time.Time
	codexFastHover                 bool
}

// The pool also owns clients created by a tea.Cmd that finishes during shutdown.
type clientPool struct {
	mu       sync.Mutex
	clients  map[*rpc.Client]bool
	closed   bool
	starting sync.WaitGroup
}

func (p *clientPool) start(c rpc.Config) (*rpc.Client, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("frontend closed")
	}
	p.starting.Add(1)
	p.mu.Unlock()
	defer p.starting.Done()
	client, err := rpc.Start(c)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	closed := p.closed
	if !closed {
		p.clients[client] = true
	}
	p.mu.Unlock()
	if closed {
		_ = client.Close()
		return nil, errors.New("frontend closed")
	}
	return client, nil
}
func (p *clientPool) close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.starting.Wait()
	p.mu.Lock()
	clients := make([]*rpc.Client, 0, len(p.clients))
	for c := range p.clients {
		clients = append(clients, c)
	}
	p.mu.Unlock()
	var wg sync.WaitGroup
	for _, c := range clients {
		wg.Add(1)
		go func(c *rpc.Client) { defer wg.Done(); _ = c.Close() }(c)
	}
	wg.Wait()
	return nil
}
func New(opts Options, cfg config.Config) *Model {
	m := &Model{opts: opts, cfg: cfg, width: 100, height: 30, pool: &clientPool{clients: map[*rpc.Client]bool{}}}
	m.quota = codex.New(cfg.AgentDir)
	m.refreshCodexFast(time.Now())
	m.updates.reader = newUpdateReader()
	m.addTab(opts.SessionFile)
	return m
}
func (m *Model) Close() error {
	m.updates.reader.close()
	m.imagePreviews.Close()
	m.fileReader.close()
	m.quota.Close()
	m.git.close()
	return m.pool.close()
}
func (m *Model) current() *tab {
	if len(m.tabs) == 0 {
		return nil
	}
	return m.tabs[m.active]
}
func (m *Model) byID(id int) *tab {
	for _, t := range m.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}
func (m *Model) addTab(file string) *tab {
	m.nextID++
	e := input.New()
	t := &tab{id: m.nextID, editor: e, dirty: true, follow: true, historyIndex: -1, folds: map[string]bool{}, statuses: map[string]string{}, widgets: map[string][]string{}}
	t.newSession = file == ""
	t.state.Cwd = m.opts.Cwd
	t.state.File = file
	t.status = "Starting pi…"
	t.showThinking = true
	if hide, ok := m.cfg.Settings["hideThinkingBlock"].(bool); ok {
		t.showThinking = !hide
	}
	m.tabs = append(m.tabs, t)
	m.activate(len(m.tabs) - 1)
	return t
}
func (m *Model) activate(i int) tea.Cmd {
	if i < 0 || i >= len(m.tabs) {
		return nil
	}
	if t := m.current(); t != nil {
		t.editor.Area.Blur()
		// An unfinished drag must never consume another session's release.
		if t.selection.dragging {
			t.selection = transcriptSelection{}
		}
		for _, d := range t.dialogs {
			d.editor.Area.Blur()
		}
	}
	m.active = i
	t := m.current()
	t.unread = 0
	t.dirty = true
	m.completionHidden = false
	m.completionIndex = 0
	m.searching = ""
	m.search = ""
	m.resizeEditor(t)
	m.revealSidebar()
	return m.focus(t)
}
func (m *Model) open(file string) tea.Cmd {
	if m.reservedSession(file) {
		if t := m.current(); t != nil {
			t.status = "Session operation in progress."
		}
		return nil
	}
	if file != "" {
		file = sessionPath(file)
		for i, t := range m.tabs {
			if sessionPath(t.state.File) == file && !t.dead {
				return m.activate(i)
			}
		}
	}
	t := m.addTab(file)
	return tea.Batch(m.start(t), t.editor.Area.Focus())
}

// A symlink/relative spelling must not open a second writer on the same JSONL.
func sessionPath(path string) string {
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(absolute); err == nil {
		return real
	}
	return absolute
}

type openedMsg struct {
	id     int
	client *rpc.Client
	err    error
}
type eventMsg struct {
	id     int
	event  rpc.Event
	closed bool
}
type replyMsg struct {
	id            int
	op            string
	data          json.RawMessage
	err           error
	text          string
	images        []map[string]any
	draftRevision uint64
}
type snapshotMsg struct {
	id                              int
	revision                        uint64
	state, entries, stats, commands json.RawMessage
	err                             error
}
type sessionsMsg struct {
	hideCurrent bool
	id          int
	list        []session.Summary
	err         error
}
type tickMsg time.Time
type noticeMsg struct {
	id   int
	text string
}

func tick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func (m *Model) start(t *tab) tea.Cmd {
	id, file := t.id, t.state.File
	opts, agentDir, pool := m.opts, m.cfg.AgentDir, m.pool
	opts.Cwd = m.tabDirectory(t)
	return func() tea.Msg {
		c, e := pool.start(rpc.Config{Bin: opts.Bin, Cwd: opts.Cwd, AgentDir: agentDir, SessionFile: file, NoSession: opts.NoSession, Args: opts.PiArgs})
		return openedMsg{id, c, e}
	}
}
func nextEvent(t *tab) tea.Cmd {
	id, c := t.id, t.client
	return func() tea.Msg { e, ok := <-c.Events(); return eventMsg{id: id, event: e, closed: !ok} }
}
func (m *Model) Init() tea.Cmd {
	t := m.current()
	return tea.Batch(m.start(t), t.editor.Area.Focus(), tick(), m.requestGit(time.Now()), m.requestSidebarHistory(time.Now()), m.releaseNotesCommand(), m.updates.reader.command(m.updateConfig()))
}
func call(t *tab, op string, data map[string]any) tea.Cmd {
	if t == nil || t.client == nil || t.dead {
		return nil
	}
	if t.mutation != nil && t.mutation.kind == "delete" {
		return nil
	}
	id, c := t.id, t.client
	if data == nil {
		data = map[string]any{}
	}
	data["type"] = op
	if !strings.HasPrefix(op, "get_") {
		t.revision++
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		raw, err := c.Call(ctx, data)
		return replyMsg{id: id, op: op, data: raw, err: err}
	}
}
func (m *Model) refresh(t *tab) tea.Cmd {
	if t.client == nil || t.dead {
		return nil
	}
	if t.refreshing {
		t.refreshAgain = true
		return nil
	}
	t.refreshing = true
	t.refreshAgain = false
	id, c, file, revision := t.id, t.client, t.state.File, t.revision
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s := snapshotMsg{id: id, revision: revision}
		s.state, s.err = c.Call(ctx, map[string]any{"type": "get_state"})
		if s.err != nil {
			return s
		}
		var current struct{ SessionFile string }
		if json.Unmarshal(s.state, &current) == nil && current.SessionFile != "" {
			file = current.SessionFile
		}
		s.entries, s.err = c.Call(ctx, map[string]any{"type": "get_entries"})
		if s.err != nil && file != "" {
			s.entries, s.err = session.ReadFile(file)
		}
		if s.err != nil {
			return s
		}
		s.stats, s.err = c.Call(ctx, map[string]any{"type": "get_session_stats"})
		if s.err != nil {
			return s
		}
		s.commands, s.err = c.Call(ctx, map[string]any{"type": "get_commands"})
		return s
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	renderNow := true
	switch v := msg.(type) {
	case footerReplyMsg:
		cmds = append(cmds, m.footerReply(v))
	case releaseNotesMsg:
		m.receiveReleaseNotes(v)
	case updateCheckMsg:
		m.receiveUpdateCheck(v)
	case sessionActionMsg:
		cmds = append(cmds, m.finishSessionAction(v))
	case tea.WindowSizeMsg:
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.revealSidebar()
		for _, t := range m.tabs {
			t.selection = transcriptSelection{}
			t.dirty = true
			m.resizeEditor(t)
		}
	case filesMsg:
		m.receiveFiles(v)
	case quotaMsg:
		m.quotaPending = false
		key, _ := m.quotaIdentity()
		if v.generation == m.quotaGeneration && v.key == key && v.key == m.quotaKey {
			m.quotaSnapshot = v.snapshot
			m.revealSidebar()
		}
	case gitStatusMsg:
		hadGit := m.sidebarContext().Git != nil
		m.gitPending = false
		m.gitCwd, m.gitInfo = v.cwd, v.info
		if v.err != nil {
			m.gitInfo = nil
		}
		if hadGit != (m.sidebarContext().Git != nil) {
			m.revealSidebar()
		}
	case openedMsg:
		t := m.byID(v.id)
		if t == nil {
			if v.client != nil {
				return m, func() tea.Msg { _ = v.client.Close(); return nil }
			}
			return m, nil
		}
		t.client = v.client
		if v.err != nil {
			t.dead = true
			t.status = v.err.Error()
		} else {
			t.status = "Ready"
			cmds = append(cmds, nextEvent(t), m.refresh(t))
		}
	case eventMsg:
		// Streaming events only invalidate the document. Tick batches Markdown work.
		renderNow = false
		t := m.byID(v.id)
		if t == nil {
			return m, nil
		}
		if v.closed {
			if t.mutation != nil {
				t.mutation.disconnected = true
			}
			t.revision++
			t.dead = true
			t.state.Busy = false
			t.sending = false
			t.status = "pi stopped · /new to start another session"
			return m, nil
		}
		if t.client != nil {
			cmds = append(cmds, nextEvent(t))
		}
		if v.event.Err != nil {
			t.status = v.event.Err.Error()
		}
		if len(v.event.Data) > 0 {
			var env struct{ Type string }
			_ = json.Unmarshal(v.event.Data, &env)
			wasWorking := t.working.active
			if env.Type == "agent_start" && !wasWorking {
				t.workingStart = len(t.state.Items)
			}
			t.working.apply(v.event.Data, time.Now())
			if env.Type == "extension_ui_request" {
				cmds = append(cmds, m.extension(t, v.event.Data))
			} else {
				t.revision++
				if t.state.Apply(v.event.Data) {
					cmds = append(cmds, m.refresh(t))
				}
				t.dirty = true
				if env.Type == "agent_start" {
					t.status = ""
					t.sending = false
					t.awaitingStart = false
					if t.interruptRequested {
						t.status = "Stopping…"
						cmds = append(cmds, call(t, "abort", nil))
					}
				}
				if env.Type == "agent_settled" {
					if wasWorking {
						t.saveAssistantStats()
					}
					t.status = "Ready"
					t.sending = false
					t.awaitingStart, t.interruptRequested = false, false
				}
				if t != m.current() && (env.Type == "message_end" || env.Type == "agent_settled") {
					t.unread++
				}
			}
		}
	case snapshotMsg:
		t := m.byID(v.id)
		if t == nil {
			return m, nil
		}
		t.refreshing = false
		if v.err != nil {
			t.status = "Refresh: " + v.err.Error()
		}
		if len(v.commands) > 0 {
			var c struct{ Commands []command }
			if err := json.Unmarshal(v.commands, &c); err == nil {
				t.commands = c.Commands
			}
		}
		if !t.dead && v.revision == t.revision {
			// A later optional endpoint failure must not throw away valid history.
			for _, part := range []struct {
				data json.RawMessage
				load func(json.RawMessage) error
			}{
				{v.state, t.state.LoadState}, {v.entries, t.state.LoadEntries}, {v.stats, t.state.LoadStats},
			} {
				if len(part.data) > 0 {
					if err := part.load(part.data); err != nil {
						t.status = err.Error()
					}
				}
			}
			t.dirty = true
			if len(t.history) == 0 {
				for _, it := range t.state.Items {
					if it.Role == "user" && strings.TrimSpace(it.Text) != "" {
						t.history = append(t.history, it.Text)
					}
				}
			}
		}
		if !t.dead && v.revision != t.revision {
			t.refreshAgain = true
		}
		// During a run, keep consuming live events rather than spin on snapshots
		// that immediately become stale. agent_settled requests the final refresh.
		if t.refreshAgain && !t.state.Busy && !t.state.Compacting {
			cmds = append(cmds, m.refresh(t))
		}
	case replyMsg:
		t := m.byID(v.id)
		if t == nil {
			return m, nil
		}
		cmds = append(cmds, m.reply(t, v))
	case sidebarHistoryMsg:
		m.sidebarHistory.pending = false
		if m.sidebarHistory.cwd != v.cwd {
			m.sidebarHistory.offset = 0
		}
		m.sidebarHistory.cwd = v.cwd
		if v.err == nil {
			m.sidebarHistory.sessions = v.sessions
		} else {
			m.sidebarHistory.sessions = nil
		}
		m.revealSidebar()
	case sessionsMsg:
		t := m.byID(v.id)
		if t == nil {
			return m, nil
		}
		if v.err != nil {
			t.status = v.err.Error()
		} else {
			m.sessionDialog(t, v.list, v.hideCurrent)
		}
	case noticeMsg:
		if t := m.byID(v.id); t != nil {
			t.notify(v.text)
		}
	case externalMsg:
		if t := m.byID(v.id); t != nil {
			if v.err != nil {
				t.status = v.err.Error()
			} else {
				t.setValue(v.text)
				m.resizeEditor(t)
			}
		}
	case imagePreviewMsg:
		if t := m.byID(v.id); t != nil {
			if v.err != nil {
				t.notify("Image preview failed: " + v.err.Error())
			} else {
				t.notify("Opened image")
			}
		}
	case clipboardImageMsg:
		if t := m.byID(v.id); t != nil {
			if v.pending {
				if v.generation != t.imageGeneration {
					break
				}
				t.imagePending = max(0, t.imagePending-1)
			}
			if v.err != nil {
				t.status = v.err.Error()
				if m.clipboardTarget == nil {
					m.clipboardTarget = t
					cmds = append(cmds, tea.ReadClipboard)
				}
			} else {
				m.attachImage(t, v.image, v.anchor)
			}
		}
	case cwdChoicesMsg:
		m.receiveCwdChoices(v)
	case tea.KeyPressMsg:
		if t := m.current(); t != nil && t.mutation != nil && t.mutation.kind == "delete" {
			break
		}
		cmds = append(cmds, m.key(v))
	case tea.PasteMsg:
		if t := m.current(); t != nil {
			if m.searching != "" && len(t.dialogs) == 0 {
				m.search += render.Clean(v.Content)
				m.find(t, 1)
				break
			}
			cmds = append(cmds, m.editTarget(t, "", v))
			m.resizeEditor(t)
		}
	case tea.ClipboardMsg:
		// OSC 52 replies have no request ID: permit one outstanding request and
		// retain its original tab, rather than pasting into whichever is now active.
		if t := m.clipboardTarget; t != nil && m.byID(t.id) == t {
			cmds = append(cmds, m.editDraft(t, "", tea.PasteMsg{Content: v.Content}))
			m.resizeEditor(t)
		}
		m.clipboardTarget = nil
	case tea.MouseClickMsg:
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		cmds = append(cmds, m.mouse(v.Mouse()))
	case tea.MouseMotionMsg:
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		m.codexFastHover = m.codexFastPopupVisible()
		m.mouseMotion(v.Mouse())
		cmds = append(cmds, m.hoverFooter())
	case tea.MouseReleaseMsg:
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		cmds = append(cmds, m.mouseRelease(v.Mouse()))
	case tea.MouseWheelMsg:
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		if m.codexFastPopupVisible() {
			break
		}
		if m.footerHoverKind(m.current()) != "" {
			delta := 0
			if v.Button == tea.MouseWheelUp {
				delta = -1
			}
			if v.Button == tea.MouseWheelDown {
				delta = 1
			}
			m.scrollFooterPopup(m.current(), delta)
			break
		}
		t := m.current()
		if t != nil {
			delta := 0
			if v.Button == tea.MouseWheelUp {
				delta = -3
			} else if v.Button == tea.MouseWheelDown {
				delta = 3
			}
			if m.sidebarWidth() > 0 && v.X >= m.mainWidth() {
				layout := m.sidebarLayout()
				if v.Y >= layout.HistoryStart && v.Y < layout.HistoryStart+layout.HistoryCount*render.SidebarHistoryRows {
					m.sidebarHistory.offset = min(max(0, layout.HistoryOffset+delta), max(0, len(m.sidebarHistory.sessions)-layout.HistoryCount))
				} else if v.Y >= layout.SessionsStart && v.Y < m.height-1 {
					visible := max(1, layout.Capacity)
					m.sidebarOffset = min(max(0, m.sidebarOffset+delta), max(0, len(m.tabs)-visible))
				}
			} else if len(t.dialogs) > 0 {
				d := t.dialogs[0]
				d.selected = max(0, d.selected+delta)
				if d.kind != "help" {
					d.selected = min(d.selected, max(0, len(m.filtered(d))-1))
				}
				m.previewTheme(d)
			} else {
				m.scroll(t, delta)
			}
		}
	case tickMsg:
		m.refreshCodexFast(time.Time(v))
		m.frame++
		now := time.Time(v)
		for _, t := range m.tabs {
			for i := len(t.dialogs) - 1; i >= 0; i-- {
				d := t.dialogs[i]
				if !d.deadline.IsZero() && now.After(d.deadline) {
					t.dialogs = append(t.dialogs[:i], t.dialogs[i+1:]...)
					cmds = append(cmds, respond(t, d, map[string]any{"cancelled": true}))
					if t == m.current() {
						cmds = append(cmds, m.focus(t))
					}
				}
			}
		}
		cmds = append(cmds, tick(), m.requestGit(now), m.requestSidebarHistory(now), m.pollQuota(now))
	default:
		if t := m.current(); t != nil {
			cmds = append(cmds, m.editTarget(t, "", msg))
		}
	}
	cmds = append(cmds, m.ensureFileCompletion(), m.ensureCwdCompletion())
	if t := m.current(); renderNow && t != nil && t.dirty {
		m.document(t)
	}
	cmds = append(cmds, m.markReleaseNotesDisplayed(), m.updatePointer())
	return m, tea.Batch(cmds...)
}
func (m *Model) document(t *tab) {
	// Freeze only the display during a drag, never the RPC event projection.
	if t.selection.dragging {
		return
	}
	assistantTitle := t.state.Model.Name
	if assistantTitle == "" {
		assistantTitle = t.state.Model.ID
	}
	if assistantTitle == "" {
		assistantTitle = "助手"
	}
	if m.showStartupInfo(t) {
		if t.doc.Text == "" {
			t.follow = false
		}
		t.doc = m.startupDocument(t)
	} else {
		t.doc = t.renderCache.Transcript(t.state.Items, m.cfg.Theme, render.Options{ShowToolActivity: t.state.Busy && !t.dead && !t.state.Compacting, AssistantTitle: assistantTitle, AssistantStats: t.assistantStats, Width: m.mainWidth(), MessageCount: t.state.MessageCount, ExpandTools: t.expanded, ShowThinking: t.showThinking, Expanded: t.folds})
	}
	if t.selection.document != t.doc.Text {
		t.selection = transcriptSelection{}
	}
	t.dirty = false
	m.clampScroll(t)
}
func (m *Model) clampScroll(t *tab) {
	bottom := max(0, len(strings.Split(t.doc.Text, "\n"))-m.transcriptHeight(t))
	if t.follow {
		t.offset = bottom
	} else {
		t.offset = min(max(0, t.offset), bottom)
	}
}
func (m *Model) scroll(t *tab, delta int) {
	t.noteScroll()
	t.follow = false
	t.offset += delta
	m.clampScroll(t)
	bottom := max(0, len(strings.Split(t.doc.Text, "\n"))-m.transcriptHeight(t))
	t.follow = t.offset >= bottom
}
func (m *Model) editTarget(t *tab, action string, msg tea.Msg) tea.Cmd {
	if len(t.dialogs) > 0 {
		d := t.dialogs[0]
		if d.kind == "help" || d.kind == "confirm" || d.kind == "close" || (d.questionnaire != nil && !d.questionnaire.Input) {
			return nil
		}
		before := d.editor.Value()
		cmd := d.editor.Update(action, msg)
		if d.editor.Value() != before {
			d.selected = 0
			m.previewTheme(d)
		}
		return cmd
	}
	return m.editDraft(t, action, msg)
}
func (m *Model) editDraft(t *tab, action string, msg tea.Msg) tea.Cmd {
	before := t.editor.Value()
	cmd := t.editor.Update(action, msg)
	if before != t.editor.Value() {
		t.dismissCompletedWorking()
		t.draftRevision++
		t.historyIndex = -1
		m.completionIndex = 0
		m.completionHidden = false
	}
	return cmd
}
func (t *tab) setValue(value string) {
	if value != t.editor.Value() {
		t.dismissCompletedWorking()
	}
	t.editor.SetValue(value)
	t.draftRevision++
}
func (m *Model) focus(t *tab) tea.Cmd {
	t.editor.Area.Blur()
	for _, d := range t.dialogs {
		d.editor.Area.Blur()
	}
	if len(t.dialogs) > 0 {
		return t.dialogs[0].editor.Area.Focus()
	}
	return t.editor.Area.Focus()
}
func (m *Model) reply(t *tab, v replyMsg) tea.Cmd {
	if v.op == "prompt" {
		t.sending = false
		if v.err != nil {
			t.awaitingStart = false
			if !t.state.Busy && !t.state.Compacting {
				t.interruptRequested = false
			}
		}
	}
	if v.err != nil {
		t.status = v.err.Error()
		if v.op == "prompt" && (v.text != "" || len(v.images) > 0) && t.draftRevision == v.draftRevision {
			t.setValue(v.text)
			t.images = v.images
			m.resizeEditor(t)
		}
		return nil
	}
	if !strings.HasPrefix(v.op, "get_") {
		t.revision++
	}
	switch v.op {
	case "get_available_models":
		var data struct{ Models []session.ModelInfo }
		_ = json.Unmarshal(v.data, &data)
		d := m.newDialog("model", "Choose model", "Type to filter")
		for _, model := range data.Models {
			d.options = append(d.options, option{Label: model.ID, Detail: model.Provider, Value: model.Provider + "/" + model.ID, Data: model})
		}
		t.dialogs = append(t.dialogs, d)
	case "get_available_thinking_levels":
		var data struct{ Levels []string }
		if err := json.Unmarshal(v.data, &data); err != nil {
			t.status = err.Error()
			return nil
		}
		d := m.newDialog("thinking", "Thinking level", "Select a thinking intensity for this session")
		for i, level := range data.Levels {
			d.options = append(d.options, option{Label: level, Value: level})
			if level == t.state.Thinking {
				d.selected = i
			}
		}
		t.dialogs = append(t.dialogs, d)
	case "get_fork_messages":
		var data struct {
			Messages []struct{ EntryID, Text string }
		}
		_ = json.Unmarshal(v.data, &data)
		d := m.newDialog("fork", "Fork from a user message", "The current session remains saved.")
		for _, v := range data.Messages {
			d.options = append(d.options, option{Label: v.Text, Value: v.EntryID})
		}
		t.dialogs = append(t.dialogs, d)
	case "get_entries":
		var data struct {
			Entries []struct {
				ID, Type string
				Message  struct {
					Role    string
					Content json.RawMessage
				}
				Summary string
			}
			LeafID string
		}
		_ = json.Unmarshal(v.data, &data)
		d := m.newDialog("tree", "Session tree · read only", "Full saved tree. RPC does not expose in-place navigation; /fork branches safely.")
		for _, e := range data.Entries {
			text := string(e.Message.Content)
			var s string
			if json.Unmarshal(e.Message.Content, &s) == nil {
				text = s
			} else {
				var blocks []struct{ Text string }
				_ = json.Unmarshal(e.Message.Content, &blocks)
				text = ""
				for _, b := range blocks {
					text += b.Text + " "
				}
			}
			if text == "" {
				text = e.Summary
			}
			marker := ""
			if e.ID == data.LeafID {
				marker = " ●"
			}
			d.options = append(d.options, option{Label: e.Message.Role + " " + text + marker, Detail: e.Type, Value: e.ID})
		}
		t.dialogs = append(t.dialogs, d)
	case "clear_queue":
		var q struct{ Steering, FollowUp []string }
		_ = json.Unmarshal(v.data, &q)
		texts := append(q.Steering, q.FollowUp...)
		if len(texts) > 0 {
			if t.editor.Value() != "" {
				texts = append(texts, t.editor.Value())
			}
			t.setValue(strings.Join(texts, "\n\n"))
			m.resizeEditor(t)
		}
		t.state.Steering = nil
		t.state.FollowUp = nil
	case "export_html":
		var p struct{ Path string }
		_ = json.Unmarshal(v.data, &p)
		t.notify("Exported: " + p.Path)
	case "prompt":
		if v.text != "" && (len(t.history) == 0 || t.history[len(t.history)-1] != v.text) {
			t.history = append(t.history, v.text)
		}
		var p struct{ Disposition string }
		_ = json.Unmarshal(v.data, &p)
		if p.Disposition == "queued" || p.Disposition == "handled" {
			t.awaitingStart = false
			if !t.state.Busy && !t.state.Compacting {
				t.interruptRequested = false
			}
		}
		if p.Disposition == "queued" {
			t.status = "Queued"
		}
		return nil
	case "fork":
		var p struct{ Text string }
		_ = json.Unmarshal(v.data, &p)
		if p.Text != "" {
			t.setValue(p.Text)
		}
		t.follow = true
	}
	return m.refresh(t)
}
func (m *Model) status(t *tab) string {
	if t.dead {
		return "● stopped · " + t.status
	}
	if len(t.dialogs) > 0 {
		return "◆ needs input"
	}
	if t.state.Compacting {
		return "◌ compacting · history retained"
	}
	if t.state.Busy {
		dots := []string{"◐", "◓", "◑", "◒"}
		return dots[(m.frame/2)%4] + " working"
	}
	if t.sending || t.awaitingStart {
		return "◌ sending"
	}
	return t.status
}
func (m *Model) sessionLabel(t *tab) string { return session.DisplayName(t.state) }
