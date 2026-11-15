package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

var localCommands = []command{
	{"new", "New parallel session"}, {"sessions", "Switch / resume a session"}, {"resume", "Resume a saved session"},
	{"model", "Select model"}, {"thinking", "Set thinking level"}, {"name", "Rename this session"},
	{"origin", "View the source session and fork turn"},
	{"tree", "Browse full saved tree"}, {"fork", "Branch from a user message"}, {"compact", "Compact model context; keep visible history"},
	{"theme", "Choose frontend color theme"}, {"hotkeys", "Effective native keybindings"}, {"reload", "Reload frontend keybindings, theme and settings"},
	{"copy", "Copy last assistant response"}, {"export", "Export conversation as HTML"}, {"attach", "Attach an image by path"}, {"detach", "Remove pending image attachments"},
	{"delete", "Delete this session permanently (confirm first)"},
	{"close", "Close this session"}, {"quit", "Exit; stop all open sessions"},
}

func (m *Model) key(k tea.KeyPressMsg) (cmd tea.Cmd) {
	defer func() {
		if next := m.ensureFileCompletion(); next != nil {
			cmd = tea.Batch(cmd, next)
		}
	}()
	t := m.current()
	if t == nil {
		return nil
	}
	chord := config.KeyChord(k)
	match := func(action string) bool { return m.cfg.Keys.Match(action, chord) }
	if len(t.dialogs) > 0 {
		return m.dialogKey(t, k, chord)
	}
	if m.inspectorOpen() && k.Code == tea.KeyEscape {
		m.backInspector()
		return nil
	}
	if m.inspector.view().Conversation && (m.inspectorFullscreen() || m.inspectorContains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY)) {
		if match("app.thinking.toggle") {
			m.inspector.content.ToggleThinking()
			return nil
		}
	}
	if match("app.interrupt") && (t.ownRunActive() || t.interruptRequested || t.resume.submit != nil) {
		return m.interrupt(t)
	}
	if match("app.exit") && t.readOnly() {
		return m.requestCloseSession(t, false)
	}
	if match("tui.input.copy") || match("app.message.copy") {
		if m.inspectorOpen() {
			if text := m.inspector.selection.text(); text != "" {
				return tea.SetClipboard(text)
			}
		}
		if text := t.selection.text(); text != "" {
			return copySelection(t, text)
		}
	}
	// Keyboard editing/navigation ends a mouse gesture; copied text never replaces a draft.
	t.selection = transcriptSelection{}
	m.inspector.selection = transcriptSelection{}
	if t.editor.Jumping() {
		return m.editDraft(t, "", k)
	}
	// Gate editor actions, including completion/history shortcuts, while leaving
	// transcript navigation and session management available.
	editing := k.Text != "" || match("app.clear") || match("app.editor.external") || match("app.clipboard.pasteImage") || match("app.message.followUp") || match("tui.input.submit") || match("tui.input.tab") || match("tui.input.newLine")
	for _, action := range m.cfg.Keys.Actions() {
		editing = editing || (strings.HasPrefix(action, "tui.editor.") && match(action))
	}
	if editing && m.searching == "" {
		if blocked, cmd := m.gateSession(t); blocked {
			return cmd
		}
	}
	if m.searching != "" {
		return m.searchKey(t, k, chord)
	}
	completions := m.completion(t)
	if len(completions) > 0 {
		switch {
		case match("tui.select.up"):
			m.completionIndex = (m.completionIndex + len(completions) - 1) % len(completions)
			return nil
		case match("tui.select.down"):
			m.completionIndex = (m.completionIndex + 1) % len(completions)
			return nil
		case match("tui.select.cancel"):
			m.completionHidden = true
			return nil
		case match("tui.input.tab"):
			m.complete(t, completions)
			return nil
		case match("tui.input.submit"):
			return m.executeCompletion(t, completions)
		}
	}
	if _, active := mentionAt(t.editor.Value(), t.editor.CursorOffset()); active && !m.completionHidden {
		if match("tui.select.cancel") {
			m.completionHidden = true
			return nil
		}
		if match("tui.input.tab") {
			if m.files.pending == "" {
				t.notify("No file matches · " + m.hint("tui.select.cancel") + " to dismiss")
			}
			return nil
		}
	}
	switch {
	case match("app.interrupt"):
		if time.Since(m.lastEscape) < 500*time.Millisecond {
			m.lastEscape = time.Time{}
			if action, _ := m.cfg.Settings["doubleEscapeAction"].(string); action == "fork" {
				return m.requestCall(t, "get_fork_messages", nil)
			}
			return m.requestCall(t, "get_entries", nil)
		}
		m.lastEscape = time.Now()
		return nil
	case match("app.clear"):
		if t.editor.Value() != "" || len(t.images) > 0 || t.imagePending > 0 {
			t.setValue("")
			t.clearImages()
			m.resizeEditor(t)
			return nil
		}
		if time.Since(m.lastClear) < time.Second {
			return tea.Quit
		}
		m.lastClear = time.Now()
		t.status = "Press " + m.hint("app.clear") + " again to exit all sessions"
		return nil
	case match("app.exit") && (t.editor.Value() == "" || t.readOnly()):
		return m.requestCloseSession(t, false)
	case match("app.suspend"):
		return tea.Suspend
	case match("app.session.new"):
		return m.open("")
	case match("app.session.previous"):
		return m.activate((m.active + len(m.tabs) - 1) % len(m.tabs))
	case match("app.session.next"):
		return m.activate((m.active + 1) % len(m.tabs))
	case match("app.session.resume"):
		return m.listSessions(t, false)
	case match("app.session.tree"):
		return m.requestCall(t, "get_entries", nil)
	case match("app.session.origin"):
		return m.requestOrigin(t)
	case match("app.session.fork"):
		return m.requestCall(t, "get_fork_messages", nil)
	case match("app.model.select"):
		return m.requestCall(t, "get_available_models", nil)
	case match("app.model.cycleForward"):
		return m.requestCall(t, "cycle_model", nil)
	case match("app.model.cycleBackward"):
		return m.cycleBackward(t)
	case match("app.thinking.cycle"):
		return m.requestCall(t, "cycle_thinking_level", nil)
	case match("app.thinking.save"):
		if t.readOnly() {
			return nil
		}
		return saveThinking(t, m.cfg.AgentDir)
	case match("app.thinking.toggle"):
		t.showThinking = !t.showThinking
		t.invalidateDocument()
		m.document(t)
		return nil
	case match("app.message.copy"):
		return m.copyLast(t)
	case match("app.editor.external"):
		return externalEditor(t)
	case match("app.message.dequeue"):
		return m.requestCall(t, "clear_queue", nil)
	case match("app.clipboard.pasteImage"):
		return m.startImagePaste(t, readClipboardImage(t.id), false)
	case match("app.message.followUp"):
		return m.submit(t, true)
	case match("tui.altScreen.pageUp"):
		m.scroll(t, -max(1, m.transcriptHeight(t)-2))
		return nil
	case match("tui.altScreen.pageDown"):
		m.scroll(t, max(1, m.transcriptHeight(t)-2))
		return nil
	case match("tui.altScreen.halfPageUp"):
		m.scroll(t, -max(1, m.transcriptHeight(t)/2))
		return nil
	case match("tui.altScreen.halfPageDown"):
		m.scroll(t, max(1, m.transcriptHeight(t)/2))
		return nil
	case match("tui.altScreen.lineUp"):
		m.scroll(t, -1)
		return nil
	case match("tui.altScreen.lineDown"):
		m.scroll(t, 1)
		return nil
	case match("tui.altScreen.top"):
		t.noteScroll()
		t.follow = false
		t.offset = 0
		return nil
	case match("tui.altScreen.bottom"):
		t.noteScroll()
		t.follow = true
		m.clampScroll(t)
		return nil
	case match("tui.altScreen.previousPrompt"):
		m.promptScroll(t, -1)
		return nil
	case match("tui.altScreen.nextPrompt"):
		m.promptScroll(t, 1)
		return nil
	case match("tui.altScreen.search"):
		m.searching = "find"
		m.search = ""
		return nil
	case match("tui.input.submit"):
		return m.submit(t, false)
	case match("tui.input.tab"):
		return m.ensureFileCompletion()
	}
	for _, action := range m.cfg.Keys.Actions() {
		if !strings.HasPrefix(action, "tui.editor.") && action != "tui.input.newLine" {
			continue
		}
		if !match(action) {
			continue
		}
		if action == "tui.editor.cursorUp" && t.editor.Area.Line() == 0 && t.editor.Area.LineInfo().RowOffset == 0 {
			m.history(t, -1)
			return nil
		}
		if action == "tui.editor.cursorDown" && t.editor.Area.Line() == t.editor.Area.LineCount()-1 && t.editor.Area.LineInfo().RowOffset == t.editor.Area.LineInfo().Height-1 && t.historyIndex >= 0 {
			m.history(t, 1)
			return nil
		}
		cmd := m.editDraft(t, action, k)
		m.resizeEditor(t)
		m.completionIndex = 0
		return cmd
	}
	before := t.editor.Value()
	cmd = m.editDraft(t, "", k)
	if before != t.editor.Value() {
		t.historyIndex = -1
		m.completionIndex = 0
		m.completionHidden = false
		m.resizeEditor(t)
	}
	return cmd
}

// interrupt is deliberately independent of completion/search state. An early
// abort is retried on agent_start while the latch is held, closing preflight races.
func (m *Model) interrupt(t *tab) tea.Cmd {
	if t.readOnly() || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.resume.submit != nil {
		m.restoreResumeSubmission(t)
		t.notify("已取消待发送输入")
		return nil
	}
	t.interruptRequested = true
	m.completionHidden = true
	m.searching = ""
	t.selection = transcriptSelection{}
	t.status = "Stopping…"
	return m.requestCall(t, "abort", nil)
}
func (m *Model) history(t *tab, delta int) {
	if len(t.history) == 0 {
		return
	}
	if t.historyIndex < 0 {
		if delta > 0 {
			return
		}
		t.historyDraft = t.editor.Value()
		t.historyIndex = len(t.history)
	}
	t.historyIndex = max(0, t.historyIndex+delta)
	if t.historyIndex >= len(t.history) {
		t.historyIndex = -1
		t.setValue(t.historyDraft)
	} else {
		t.setValue(t.history[t.historyIndex])
	}
	m.resizeEditor(t)
}
func (m *Model) promptScroll(t *tab, direction int) {
	t.noteScroll()
	if direction < 0 {
		for i := len(t.doc.Blocks) - 1; i >= 0; i-- {
			b := t.doc.Blocks[i]
			if b.Role == "user" && b.Start < t.offset {
				t.offset = b.Start
				t.follow = false
				return
			}
		}
		t.offset = 0
		t.follow = false
	} else {
		for _, b := range t.doc.Blocks {
			if b.Role == "user" && b.Start > t.offset {
				t.offset = b.Start
				t.follow = false
				m.clampScroll(t)
				return
			}
		}
		t.follow = true
		m.clampScroll(t)
	}
}
func (m *Model) searchKey(t *tab, k tea.KeyPressMsg, chord string) tea.Cmd {
	match := func(a string) bool { return m.cfg.Keys.Match(a, chord) }
	switch {
	case match("tui.altScreen.searchClose"):
		m.searching = ""
	case match("tui.altScreen.searchNext") || match("tui.input.submit"):
		m.find(t, 1)
	case match("tui.altScreen.searchPrevious"):
		m.find(t, -1)
	case match("tui.editor.deleteCharBackward"):
		r := []rune(m.search)
		if len(r) > 0 {
			m.search = string(r[:len(r)-1])
		}
	default:
		if k.Text != "" && k.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModSuper|tea.ModMeta|tea.ModHyper) == 0 {
			m.search += k.Text
			m.find(t, 1)
		}
	}
	return nil
}
func (m *Model) find(t *tab, direction int) {
	if m.search == "" {
		return
	}
	lines := strings.Split(ansi.Strip(t.doc.Text), "\n")
	for n := 1; n <= len(lines); n++ {
		i := (t.offset + direction*n) % len(lines)
		if i < 0 {
			i += len(lines)
		}
		if strings.Contains(strings.ToLower(lines[i]), strings.ToLower(m.search)) {
			t.offset = i
			t.follow = false
			m.clampScroll(t)
			return
		}
	}
	t.status = "No match: " + m.search
}
func (m *Model) copyLast(t *tab) tea.Cmd {
	for i := len(t.state.Items) - 1; i >= 0; i-- {
		it := t.state.Items[i]
		if it.Role == "assistant" && it.Text != "" {
			t.notify("Copied")
			return tea.SetClipboard(it.Text)
		}
	}
	t.notify("No assistant response to copy")
	return nil
}
func (m *Model) listSessions(t *tab, hideCurrent bool) tea.Cmd {
	id, root, cwd := t.id, m.cfg.AgentDir, m.tabDirectory(t)
	return func() tea.Msg {
		list, err := session.List(root, cwd)
		return sessionsMsg{id: id, list: list, err: err, hideCurrent: hideCurrent}
	}
}
func (m *Model) submit(t *tab, follow bool) tea.Cmd {
	return m.submitChecked(t, follow, false)
}

func (m *Model) submitChecked(t *tab, follow, checked bool) tea.Cmd {
	if t.readOnly() || t.cycle.phase == cycleClosing || t.mutation != nil {
		return nil
	}
	if t.resume.submit != nil || t.cycle.intent != nil || t.cycle.phase == cycleStarting {
		t.notify("An operation is waiting for this session; draft retained")
		return nil
	}
	if t.sending {
		t.notify("Sending; wait before appending more content")
		return nil
	}
	if t.forkPending {
		return nil
	}
	if !checked && t.client != nil && !t.resume.pending && t.state.File != "" && !t.ownRunActive() {
		if t.sessionGuard.path == t.state.File && t.sessionGuard.warning != "" && !t.sessionGuard.approved {
			return m.sessionGateDialog(t)
		}
		return m.inspectSession(t, true, follow, time.Now())
	}
	original := t.editor.Value()
	text, attached := referencedImages(strings.TrimSpace(original), t.images)
	if text == "" && len(attached) == 0 {
		return nil
	}
	if strings.HasPrefix(text, "/") {
		if cmd, ok := m.slash(t, text); ok {
			if t.cycle.intent != nil {
				t.cycle.intent.draft = &resumeSubmission{original: original}
			}
			t.setValue("")
			m.resizeEditor(t)
			m.completionHidden = false
			return cmd
		}
	}
	if t.imagePending > 0 {
		t.notify("Reading clipboard image; wait before sending")
		return nil
	}
	if t.client == nil || t.dead {
		m.deferResumeSubmission(t, follow)
		return m.beginCycle(t, nil)
	}
	if t.resume.pending {
		return m.deferResumeSubmission(t, follow)
	}
	return m.sendPrompt(t, original, t.images, follow, true)
}

// Deferred local intent uses its captured draft, never another active editor.
func (m *Model) sendPrompt(t *tab, original string, images []map[string]any, follow, clearEditor bool) tea.Cmd {
	if t.state.Busy || t.state.Compacting || len(t.state.Steering) > 0 || len(t.state.FollowUp) > 0 {
		m.clearPromptEcho(t, t.promptEcho)
	}
	m.showPromptEcho(t, original, images, nil)
	echo := t.dispatchPromptEcho()
	text, attached := referencedImages(strings.TrimSpace(original), images)
	id, c, generation := t.id, t.client, t.cycle.generation
	mergePending := t.state.Busy && !strings.HasPrefix(text, "/")
	payload := map[string]any{"type": "prompt", "message": text}
	if t.state.Busy {
		if follow {
			payload["streamingBehavior"] = "followUp"
		} else {
			payload["streamingBehavior"] = "steer"
		}
	}
	if len(attached) > 0 {
		payload["images"] = attached
	}
	if clearEditor {
		t.clearImages()
		t.setValue("")
		m.resizeEditor(t)
		t.historyIndex = -1
	}
	draftRevision := t.draftRevision
	t.cycle.inflight++
	t.sending = true
	t.awaitingStart = true
	t.follow = true
	m.completionHidden = false
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		restoreDraft, raw, err := c.SubmitPrompt(ctx, payload, original, mergePending)
		if restoreDraft != "" {
			original = restoreDraft
		}
		// Unknown completion may omit the runtime recovery text. Keep the
		// original local draft for manual recovery, never an automatic retry.
		return replyMsg{id: id, client: c, generation: generation, op: "prompt", data: raw, err: err, text: original, images: images, draftRevision: draftRevision, deferred: !clearEditor, echo: echo}
	}
}
func (m *Model) slash(t *tab, text string) (tea.Cmd, bool) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "new":
		return m.open(""), true
	case "sessions", "resume":
		return m.listSessions(t, false), true
	case "model":
		return m.requestCall(t, "get_available_models", nil), true
	case "thinking":
		if arg == "" {
			return m.requestCall(t, "cycle_thinking_level", nil), true
		}
		return m.requestCall(t, "set_thinking_level", map[string]any{"level": arg}), true
	case "name":
		if arg != "" {
			return m.requestCall(t, "set_session_name", map[string]any{"name": arg}), true
		}
		d := m.newDialog("name", "Session name", "")
		d.editor.SetValue(t.state.Name)
		t.dialogs = append(t.dialogs, d)
		return nil, true
	case "tree":
		return m.requestCall(t, "get_entries", nil), true
	case "origin":
		return m.requestOrigin(t), true
	case "fork":
		return m.requestCall(t, "get_fork_messages", nil), true
	case "compact":
		return m.requestCall(t, "compact", map[string]any{"customInstructions": arg}), true
	case "copy":
		return m.copyLast(t), true
	case "export":
		p := map[string]any{}
		if arg != "" {
			p["outputPath"] = arg
		}
		return m.requestCall(t, "export_html", p), true
	case "attach":
		if arg == "" {
			t.status = "Usage: /attach path/to/image.png"
			return nil, true
		}
		p := arg
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.tabDirectory(t), p)
		}
		id := t.id
		return m.startImagePaste(t, func() tea.Msg { image, err := loadImage(p); return clipboardImageMsg{id: id, image: image, err: err} }, true), true
	case "detach":
		t.clearImages()
		t.draftRevision++
		t.notify("Attachments removed")
		return nil, true
	case "hotkeys", "shortcuts", "help":
		d := m.newDialog("help", "Effective keybindings", "")
		d.editor.SetValue(m.hotkeysText())
		t.dialogs = append(t.dialogs, d)
		return nil, true
	case "reload":
		cfg, err := config.Load(m.tabDirectory(t))
		if err != nil {
			t.status = err.Error()
		} else {
			m.cfg = cfg
			for _, other := range m.tabs {
				other.invalidateDocument()
				m.resizeEditor(other)
			}
			t.notify("UI config reloaded · reopen a session to reload pi extensions")
		}
		return nil, true
	case "delete":
		if arg != "" {
			t.notify("Usage: /delete — deletes only the current session after confirmation")
			return nil, true
		}
		return m.requestDeleteSession(t), true
	case "close":
		return m.requestCloseSession(t, true), true
	case "quit", "exit":
		return tea.Quit, true
	case "theme":
		return m.chooseTheme(t, arg), true
	case "settings", "scoped-models", "login", "logout", "share", "changelog":
		t.status = "/" + name + " is a native interactive-pi command, not exposed over RPC. Use pi to manage it."
		return nil, true
	}
	return nil, false
}
func (m *Model) cycleBackward(t *tab) tea.Cmd {
	if t.readOnly() || t.cycle.phase == cycleClosing {
		return nil
	}
	if t.client == nil || t.dead || t.resume.pending {
		return m.beginCycle(t, &writeIntent{run: func() tea.Cmd { return m.cycleBackward(t) }})
	}
	if t.client == nil || t.dead {
		return nil
	}
	c, id, current, generation := t.client, t.id, t.state.Model, t.cycle.generation
	t.cycle.inflight++
	patterns, _ := m.cfg.Settings["enabledModels"].([]any)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		raw, err := c.Call(ctx, map[string]any{"type": "get_available_models"})
		if err != nil {
			return replyMsg{id: id, client: c, generation: generation, op: "set_model", err: err}
		}
		var data struct{ Models []session.ModelInfo }
		if err = json.Unmarshal(raw, &data); err != nil {
			return replyMsg{id: id, client: c, generation: generation, op: "set_model", err: err}
		}
		models := data.Models
		if len(patterns) > 0 {
			var filtered []session.ModelInfo
			seen := map[string]bool{}
			for _, p := range patterns {
				pattern, _ := p.(string)
				if j := strings.LastIndex(pattern, ":"); j >= 0 {
					pattern = pattern[:j]
				}
				for _, model := range models {
					full := model.Provider + "/" + model.ID
					a, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(full))
					b, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(model.ID))
					if (a || b) && !seen[full] {
						filtered = append(filtered, model)
						seen[full] = true
					}
				}
			}
			models = filtered
		}
		if len(models) == 0 {
			return replyMsg{id: id, client: c, generation: generation, op: "set_model", err: fmt.Errorf("no models in the configured cycle")}
		}
		index := 0
		for i, model := range models {
			if model.ID == current.ID && model.Provider == current.Provider {
				index = i
				break
			}
		}
		next := models[(index+len(models)-1)%len(models)]
		raw, err = c.Call(ctx, map[string]any{"type": "set_model", "provider": next.Provider, "modelId": next.ID})
		return replyMsg{id: id, client: c, generation: generation, op: "set_model", data: raw, err: err}
	}
}
