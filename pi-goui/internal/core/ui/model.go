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
	"pi-goui/internal/core/codex"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/input"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/core/ui/imagepreview"
	rpc "pi-goui/internal/runtime"
)

type Options struct {
	Cwd, Bin, SessionFile string
	NoSession             bool
	PiArgs                []string
}
type tab struct {
	extensions                                                                     extensionSession
	footerHover                                                                    footerHoverState
	footerCatalog                                                                  *footerCatalog
	newSession                                                                     bool
	mutation                                                                       *sessionAction
	awaitingStart, interruptRequested                                              bool
	id                                                                             int
	client                                                                         *rpc.Client
	lastEventErr                                                                   error // Scoped to client; reset when a connection opens.
	state                                                                          session.State
	editor                                                                         input.Editor
	doc                                                                            render.Document
	nativeDocument                                                                 render.Document
	promptEcho                                                                     *promptEcho
	renderCache                                                                    render.TranscriptCache
	inspectIDs                                                                     map[string]int
	inspectFrame                                                                   *render.Block
	inspectBottom                                                                  int
	requestedWidth                                                                 int
	layoutHasInspector                                                             bool
	docLinesText                                                                   string
	docLines                                                                       []string
	streamDirty                                                                    bool
	layoutGeneration                                                               uint64
	documentJob                                                                    *documentJob
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
	cycle                                                                          sessionCycle

	// revision protects asynchronous snapshots; draftRevision protects failed submissions.
	revision, draftRevision uint64
	title                   string
	selection               transcriptSelection
	scrollbarUntil          time.Time
	working                 workingState
	workingStart            int
	assistantStats          map[string]string
	toast                   toastState
	forkPending             bool
	forkSequence            uint64
	origin                  session.Header
	originJump              *branchJump
	sessionGuard            sessionGuard
	resume                  resumeConnection
}
type command struct{ Name, Description string }
type dialog struct {
	themeBefore                      *config.Theme
	cwd                              *cwdCompletion
	extension                        dialogExtension
	sessionSend                      *sessionGuardMsg
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
	panelResize                    panelResizeState
	pointerShape                   string
	inspector                      toolInspector
	sidebarHistory                 sidebarHistoryState
	resumePreload                  resumePreloadState
	branchEpoch                    uint64
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
	imagePreviews                  imagePreviewer
	lastEscape, lastClear          time.Time
	frame                          int
	extensions                     *extensionRuntime
	extensionControls              extensionControls
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

func (p *clientPool) closeClient(c *rpc.Client) error {
	err := c.Close()
	if err == nil {
		p.mu.Lock()
		delete(p.clients, c)
		p.mu.Unlock()
	}
	return err
}
func New(opts Options, cfg config.Config) *Model {
	m := &Model{opts: opts, cfg: cfg, width: 100, height: 30, pool: &clientPool{clients: map[*rpc.Client]bool{}}}
	m.imagePreviews = &imagepreview.Store{}
	m.quota = codex.New(cfg.AgentDir)
	m.updates.reader = newUpdateReader()
	m.addTab(opts.SessionFile)
	return m
}
func (m *Model) Close() error {
	m.updates.reader.close()
	if m.imagePreviews != nil {
		m.imagePreviews.Close()
	}
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
	m.initExtensions(t)
	t.newSession = file == ""
	t.state.Cwd = m.opts.Cwd
	t.state.File = file
	t.status = "Idle"
	if file != "" {
		t.status = "Idle"
	}
	t.showThinking = true
	if hide, ok := m.cfg.Settings["hideThinkingBlock"].(bool); ok {
		t.showThinking = !hide
	}
	m.tabs = append(m.tabs, t)
	m.activate(len(m.tabs) - 1)
	return t
}
func (m *Model) activate(i int) tea.Cmd {
	m.branchEpoch++
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
	m.finishPanelResize()
	m.inspector = toolInspector{}
	m.active = i
	m.restoreSidebarHistory()
	t := m.current()
	t.unread = 0
	t.invalidateDocument()
	m.completionHidden = false
	m.completionIndex = 0
	m.searching = ""
	m.search = ""
	m.resizeEditor(t)
	m.revealSidebar()
	t.cycle.probed = time.Time{}
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
	return tea.Batch(m.start(t), m.focus(t))
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
	generation uint64
	id         int
	client     *rpc.Client
	err        error
}
type eventMsg struct {
	id     int
	client *rpc.Client
	event  rpc.Event
	closed bool
}
type replyMsg struct {
	client        *rpc.Client
	generation    uint64
	file          string
	id            int
	op            string
	data          json.RawMessage
	err           error
	text          string
	images        []map[string]any
	draftRevision uint64
	deferred      bool // Local intent captured before the first authoritative snapshot.
	echo          *promptEcho
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
	if m.opts.NoSession {
		return m.beginCycle(t, nil)
	}
	return tea.Batch(m.pollSession(t, time.Now(), true), m.previewSession(t))
}
func nextEvent(t *tab) tea.Cmd {
	return nextEventCommand(t.id, t.client)
}
func (m *Model) Init() tea.Cmd {
	t := m.current()
	return tea.Batch(m.start(t), m.focus(t), tick(), m.requestGit(time.Now()), m.requestSidebarHistory(time.Now()), m.releaseNotesCommand(), m.updates.reader.command(m.updateConfig()))
}
func call(t *tab, op string, data map[string]any) tea.Cmd {
	if t == nil || t.client == nil || t.dead || t.readOnly() || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.mutation != nil && t.mutation.kind == "delete" {
		return nil
	}
	id, c, generation := t.id, t.client, t.cycle.generation
	if data == nil {
		data = map[string]any{}
	}
	data["type"] = op
	if !strings.HasPrefix(op, "get_") {
		t.revision++
	}
	t.cycle.inflight++
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		raw, err := c.Call(ctx, data)
		return replyMsg{id: id, client: c, generation: generation, op: op, data: raw, err: err}
	}
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Single-event messages use the same path as transport batches.
	if event, ok := msg.(eventMsg); ok {
		batch := eventBatchMsg{id: event.id, client: event.client, closed: event.closed}
		if event.closed {
			batch.err = event.event.Err
		} else {
			batch.events = []rpc.Event{event.event}
		}
		msg = batch
	}
	var cmds []tea.Cmd
	originX := m.mainScreenX()
	renderNow := true
	switch v := msg.(type) {
	case documentResult:
		m.receiveDocument(v)
	case footerReplyMsg:
		cmds = append(cmds, m.footerReply(v))
	case releaseNotesMsg:
		m.receiveReleaseNotes(v)
	case updateCheckMsg:
		m.receiveUpdateCheck(v)
	case sessionActionMsg:
		cmds = append(cmds, m.finishSessionAction(v))
	case tea.BlurMsg:
		m.finishPanelResize()
	case tea.WindowSizeMsg:
		m.panelResize.dragging = false
		m.width = max(1, v.Width)
		m.height = max(1, v.Height)
		m.revealSidebar()
		for _, t := range m.tabs {
			t.selection = transcriptSelection{}
			t.invalidateDocument()
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
	case resumeSubmitReadyMsg:
		cmds = append(cmds, m.resumeSubmissionReady(v))
	case resumePreloadMsg:
		cmds = append(cmds, m.receiveResumePreload(v))
	case resumePreviewMsg:
		cmds = append(cmds, m.receiveResumePreview(v))
		renderNow = false
	case sessionPresenceMsg:
		cmds = append(cmds, m.sessionPresence(v))
		renderNow = false
	case releaseCheckMsg:
		cmds = append(cmds, m.releaseChecked(v))
	case cycleClosedMsg:
		cmds = append(cmds, m.cycleClosed(v))
	case cyclePrimedMsg:
		cmds = append(cmds, m.cyclePrimed(v))
	case openedMsg:
		cmds = append(cmds, m.cycleOpened(v))
	case eventBatchMsg:
		// Streaming events only invalidate the document. Tick batches Markdown work.
		renderNow = false
		t := m.byID(v.id)
		if t == nil || (v.client != nil && v.client != t.client) {
			return m, nil
		}
		var eventCmds []tea.Cmd
		eventCmds, renderNow = m.applyEventBatch(t, v.events)
		if !v.closed && t.client != nil {
			cmds = append(cmds, nextEventCommand(t.id, t.client))
		}
		cmds = append(cmds, eventCmds...)
		if v.closed {
			cmds = append(cmds, m.closeEventStream(t, v.err))
			if len(v.events) == 0 {
				return m, tea.Batch(cmds...)
			}
		}
	case forkSnapshotMsg:
		cmds = append(cmds, m.forkSnapshot(v))
	case forkCreatedMsg:
		cmds = append(cmds, m.forkCreated(v))
	case branchOpenMsg:
		cmds = append(cmds, m.branchOpenResult(v))
	case snapshotMetadataMsg:
		m.applySnapshotMetadata(v)
	case snapshotMsg:
		t := m.byID(v.id)
		if t == nil || (v.client != nil && (t.client != v.client || t.state.File != v.file || t.state.ID != v.sourceID)) {
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, m.applySessionSnapshot(t, v)...)
	case sessionGuardMsg:
		cmds = append(cmds, m.sessionGuardResult(v))
	case replyMsg:
		t := m.byID(v.id)
		if t == nil || (v.client != nil && (t.client != v.client || t.cycle.generation != v.generation)) || (v.client == nil && (t.cycle.generation != v.generation || (v.file != "" && t.state.File != v.file))) {
			return m, nil
		}
		if v.client != nil {
			t.cycle.inflight = max(0, t.cycle.inflight-1)
		}
		cmds = append(cmds, m.reply(t, v))
	case sidebarHistoryMsg:
		if v.generation != m.sidebarHistory.generation {
			return m, nil
		}
		m.sidebarHistory.pending = false
		if m.sidebarHistory.cwd != v.cwd {
			m.sidebarHistory.offset = 0
		}
		m.sidebarHistory.cwd = v.cwd
		if v.err == nil {
			m.sidebarHistory.sessions = v.sessions
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
			} else if blocked, cmd := m.gateSession(t); blocked {
				cmds = append(cmds, cmd)
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
				if blocked, cmd := m.gateSession(t); blocked {
					cmds = append(cmds, cmd)
					break
				}
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
		v.X -= originX
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		cmds = append(cmds, m.mouse(v.Mouse()))
	case tea.MouseMotionMsg:
		v.X -= originX
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		m.extensionControls.Hover = m.extensionPopupVisible()
		m.mouseMotion(v.Mouse())
		cmds = append(cmds, m.hoverFooter())
	case tea.MouseReleaseMsg:
		v.X -= originX
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		cmds = append(cmds, m.mouseRelease(v.Mouse()))
	case tea.MouseWheelMsg:
		v.X -= originX
		m.sidebarHistory.pointerKnown, m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = true, v.X, v.Y
		if m.extensionPopupVisible() {
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
		if m.inspectorWheel(v.Mouse()) {
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
			if len(t.dialogs) > 0 && t.dialogs[0].extension.MultilineInput() {
				d := t.dialogs[0]
				p := m.popupGeometry(d)
				y := p.y
				if p.framed {
					y += 2 + len(p.message)
				}
				if v.X >= p.x && v.X < p.x+p.width && v.Y >= y && v.Y < y+p.inputRows {
					d.editor.Scroll(delta)
					break
				}
			}
			if len(t.dialogs) == 0 && !m.inspectorFullscreen() && v.X >= 0 && v.X < m.mainWidth() && v.Y >= m.editorTop(t) && v.Y < m.height {
				t.editor.Scroll(delta)
				break
			}
			if m.sideContainsX(v.X, 0) {
				layout := m.sidebarLayout()
				if m.scrollExtensions(t, layout, v.Y, delta) {
					// The adapter consumed the wheel event.
				} else if v.Y >= layout.HistoryStart && v.Y < layout.HistoryStart+layout.HistoryCount*render.SidebarHistoryRows {
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
		cmds = append(cmds, m.onTick(time.Time(v))...)
	default:
		if cmd, handled := m.extensionMessage(msg); handled {
			cmds = append(cmds, cmd)
			break
		}
		if t := m.current(); t != nil {
			cmds = append(cmds, m.editTarget(t, "", msg))
		}
	}
	for _, t := range m.tabs {
		cmds = append(cmds, t.syncExtensions(), m.checkRelease(t, time.Now()))
	}
	if m.panelResize.dragging && !m.panelResizeValid() {
		m.finishPanelResize()
	}
	if m.inspector.itemID != "" && !m.inspectorOpen() {
		m.closeInspector()
	}
	// Keep a stationary physical pointer aligned when the side pane moves or resizes.
	if m.sidebarHistory.pointerKnown {
		m.sidebarHistory.pointerX += originX - m.mainScreenX()
	}
	cmds = append(cmds, m.ensureFileCompletion(), m.ensureCwdCompletion(), m.ensureResumePreload())
	if t := m.current(); renderNow && t != nil && t.dirty {
		if t.streamDirty {
			if _, tick := msg.(tickMsg); tick {
				cmds = append(cmds, m.scheduleDocument(t))
			}
		} else {
			m.document(t)
		}
	}
	cmds = append(cmds, m.markReleaseNotesDisplayed(), m.updatePointer())
	return m, tea.Batch(cmds...)
}
func (m *Model) onTick(now time.Time) []tea.Cmd {
	var cmds []tea.Cmd
	m.frame++
	for _, t := range m.tabs {
		if t == m.current() {
			cmds = append(cmds, m.pollSession(t, now, false))
		}
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
	cmds = append(cmds, m.pollExtensions(now), tick(), m.requestGit(now), m.requestSidebarHistory(now), m.pollQuota(now))
	return cmds
}

func (m *Model) document(t *tab) {
	// Freeze only the display during a drag, never the RPC event projection.
	if t.selection.dragging || m.panelResize.dragging {
		return
	}
	// Large pane transitions must paint controls immediately. The previous
	// immutable transcript stays clipped to the new viewport until the next
	// layout tick; generation checks reject any old-width work, even on Esc.
	detail, width := m.inspectorOpen(), m.mainWidth()
	oldWidth := t.requestedWidth
	t.requestedWidth = width
	if t.layoutHasInspector != detail || (detail && !m.inspector.layoutSeen) {
		t.layoutHasInspector, m.inspector.layoutSeen = detail, true
		if width == oldWidth && (!t.dirty || t.streamDirty) {
			return // same-width detail switch; no transcript work
		}
		if len(t.doc.Text) >= 128<<10 || len(t.doc.Blocks) >= 512 {
			t.invalidateDocument()
			t.streamDirty = true
			return
		}
	}
	t.inspectFrame = nil
	t.layoutGeneration++ // Invalidate any older asynchronous layout.
	var doc render.Document
	if m.showStartupInfo(t) {
		if t.doc.Text == "" {
			t.follow = false
		}
		doc = m.startupDocument(t)
	} else {
		doc = t.renderCache.Transcript(t.state.Items, m.cfg.Theme, m.documentOptions(t))
	}
	t.dirty, t.streamDirty = false, false
	m.publishDocument(t, doc, nil)
}
func (m *Model) clampScroll(t *tab) {
	bottom := max(0, len(t.documentLines())-m.transcriptHeight(t))
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
	bottom := max(0, len(t.documentLines())-m.transcriptHeight(t))
	t.follow = t.offset >= bottom
}
func (m *Model) editTarget(t *tab, action string, msg tea.Msg) tea.Cmd {
	if len(t.dialogs) > 0 {
		d := t.dialogs[0]
		if d.kind == "help" || d.kind == "confirm" || d.kind == "close" || d.kind == "session-send-confirm" || !d.extension.FocusInput() {
			return nil
		}
		before := d.editor.Value()
		cmd := d.editor.Update(action, msg)
		if d.extension.MultilineInput() {
			m.resizeDialog(d)
		}
		if d.editor.Value() != before {
			d.selected = 0
			m.previewTheme(d)
		}
		return cmd
	}
	return m.editDraft(t, action, msg)
}
func (m *Model) editDraft(t *tab, action string, msg tea.Msg) tea.Cmd {
	if t.readOnly() {
		return nil
	}
	if blocked, cmd := m.gateSession(t); blocked {
		return cmd
	}
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
	if t.readOnly() {
		return nil
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
		if v.op == "prompt" {
			// In particular, an unknown completion must be visible while busy.
			// Restore for manual recovery only; never automatically resubmit.
			t.notify(v.err.Error())
		}
		if v.op == "prompt" && v.echo != nil {
			m.restorePromptEcho(t, v.echo)
		} else if v.op == "prompt" && v.deferred {
			// This intent was captured before newer typing. Keep both drafts
			// for manual recovery, including unknown completion; never retry.
			m.restoreResumeDraft(t, &resumeSubmission{original: v.text, images: v.images})
		} else if v.op == "prompt" && (v.text != "" || len(v.images) > 0) && t.draftRevision == v.draftRevision {
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
		choices := forkChoices(t.state.Items)
		d := m.newDialog("fork", "从此轮fork · 选择用户消息", "最新在前；选择后直接分叉，包含本轮回复及工具结果。")
		for i := len(data.Messages) - 1; i >= 0; i-- {
			v := data.Messages[i]
			d.options = append(d.options, choices[v.EntryID].choice(v.EntryID, v.Text))
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
			m.clearPromptEcho(t, v.echo)
			t.awaitingStart = false
			if !t.state.Busy && !t.state.Compacting {
				t.interruptRequested = false
			}
		}
		if p.Disposition == "queued" {
			t.status = "Queued"
		}
		return nil
	}
	return m.refresh(t)
}
func (m *Model) status(t *tab) string {
	if t.readOnly() {
		return t.ownershipStatus()
	}
	if t.cycle.phase == cycleClosing {
		return "Finishing session…"
	}
	if t.resume.failed {
		return "● " + t.status
	}
	if t.dead {
		return "● stopped · " + t.status
	}
	if len(t.dialogs) > 0 {
		return "◆ needs input"
	}
	if t.state.Compacting {
		return "◌ compacting · history retained"
	}
	if t.cycle.phase == cycleStarting || t.resume.submit != nil || t.sending || t.awaitingStart {
		return "working"
	}
	if t.state.Busy {
		dots := []string{"◐", "◓", "◑", "◒"}
		return dots[(m.frame/2)%4] + " working"
	}
	return t.status
}
func (m *Model) sessionLabel(t *tab) string { return session.DisplayName(t.state) }
