package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/input"
	"pi-goui/internal/core/session"
)

func (m *Model) newDialog(kind, title, message string) *dialog {
	e := input.New()
	e.Area.SetStyles(m.editorStyles())
	e.Area.Focus()
	d := &dialog{kind: kind, title: title, message: message, editor: e}
	m.resizeDialog(d)
	return d
}
func (m *Model) filtered(d *dialog) []option {
	if d.kind == "cwd" {
		if d.cwd == nil || d.cwd.pending || d.cwd.query != d.editor.Value() {
			return nil
		}
		return d.options
	}
	if d.extension.Unfiltered() {
		return d.options
	}
	if d.kind == "input" || d.kind == "editor" || d.kind == "name" || d.kind == "session-rename" || d.kind == "help" {
		return nil
	}
	query := strings.ToLower(d.editor.Value())
	if d.kind == "session" {
		query = strings.TrimSpace(query)
	}
	if d.kind == "confirm" || d.kind == "close" || d.kind == "resume" || d.kind == "session-delete" || d.kind == "session-send-confirm" {
		query = ""
	}
	var options []option
	for _, o := range d.options {
		if d.namedOnly {
			s, ok := o.Data.(session.Summary)
			if ok && s.Name == "" {
				continue
			}
		}
		search := o.Label + " " + o.Detail
		if d.kind == "session" {
			if s, ok := o.Data.(session.Summary); ok {
				search += " " + s.FirstMessage + " " + s.Path
			}
		}
		if !strings.Contains(strings.ToLower(search), query) {
			continue
		}
		if d.kind == "session" && !d.showPath {
			o.Detail = ""
		}
		options = append(options, o)
	}
	if d.reverseSort {
		sort.SliceStable(options, func(i, j int) bool { return options[i].Label < options[j].Label })
	} else if d.kind == "session" && query == "" {
		list := make([]session.Summary, len(options))
		for i, o := range options {
			list[i], _ = o.Data.(session.Summary)
		}
		threaded := make([]option, 0, len(options))
		for _, row := range session.ThreadRows(list) {
			o := options[row.Index]
			o.Label = session.ThreadPrefix(row.Depth) + o.Label
			threaded = append(threaded, o)
		}
		return threaded
	}
	return options
}
func (m *Model) sessionDialog(t *tab, list []session.Summary, hideCurrent ...bool) {
	d := m.newDialog("session", "Sessions", m.sessionSelectorHelp())
	d.hideCurrent = len(hideCurrent) > 0 && hideCurrent[0]
	seen := map[string]bool{}
	for _, other := range m.tabs {
		seen[other.state.File] = true
		if d.hideCurrent && other == t {
			continue
		}
		label := m.sessionLabel(other)
		status := "open"
		if other.state.Busy {
			status = "working"
		}
		if len(other.dialogs) > 0 {
			status = "needs input"
		}
		if other == t {
			status = "current"
		}
		summary := summaryOf(other)
		summary.Name = other.state.Name
		for _, saved := range list {
			if sameSessionPath(saved.Path, summary.Path) && saved.ID == summary.ID {
				summary.Modified, summary.FirstMessage = saved.Modified, saved.FirstMessage
				// The selector was just read from disk. A matching cached header
				// can still have stale ancestry, so prefer the fresh relationship.
				summary.ParentSession, summary.Fork = saved.ParentSession, saved.Fork
				break
			}
		}
		d.options = append(d.options, option{Label: label + "  [" + status + "]", Detail: other.state.File, Value: fmt.Sprintf("@%d", other.id), Data: summary})
	}
	for _, s := range list {
		if seen[s.Path] {
			continue
		}
		label := s.Name
		if label == "" {
			label = s.FirstMessage
		}
		if label == "" {
			label = s.ID
		}
		d.options = append(d.options, option{Label: label, Detail: s.Path, Value: s.Path, Data: s})
	}
	t.dialogs = append(t.dialogs, d)
}
func (m *Model) dialogKey(t *tab, k tea.KeyPressMsg, chord string) tea.Cmd {
	d := t.dialogs[0]
	defer func() {
		if len(t.dialogs) > 0 && t.dialogs[0] == d {
			m.previewTheme(d)
		}
	}()
	if cmd, handled := m.extensionKey(t, d, k); handled {
		return cmd
	}
	match := func(a string) bool { return m.cfg.Keys.Match(a, chord) }
	if d.editor.Jumping() {
		return d.editor.Update("", k)
	}
	if match("tui.select.cancel") {
		if cmd, handled := m.extensionCancel(t, d); handled {
			return cmd
		}
		m.restoreTheme(d)
		if d.kind == "branch-loading" {
			m.branchEpoch++
			t.originJump = nil
		}
		t.dialogs = t.dialogs[1:]
		return tea.Batch(respond(t, d, map[string]any{"cancelled": true}), m.focus(t))
	}
	if d.kind == "session" {
		switch {
		case match("app.session.toggleNamedFilter"):
			d.namedOnly = !d.namedOnly
			d.selected = 0
			return nil
		case match("app.session.togglePath"):
			d.showPath = !d.showPath
			return nil
		case match("app.session.toggleSort"):
			d.reverseSort = !d.reverseSort
			d.selected = 0
			return nil
		case match("app.session.delete"):
			return m.requestSessionAction(t, d, "delete")
		case match("app.session.rename"):
			return m.requestSessionAction(t, d, "rename")
		}
	}
	options := m.filtered(d)
	page := max(1, m.popupGeometry(d).count)
	if d.kind == "help" {
		switch {
		case match("tui.select.up"):
			d.selected = max(0, d.selected-1)
		case match("tui.select.down"):
			d.selected++
		case match("tui.select.pageUp"):
			d.selected = max(0, d.selected-page)
		case match("tui.select.pageDown"):
			d.selected += page
		case match("tui.select.confirm"):
			t.dialogs = t.dialogs[1:]
			return m.focus(t)
		}
		d.selected = min(d.selected, max(0, len(popupHelpLines(d, m.popupGeometry(d).inner))-page))
		return nil
	}
	multiline := d.kind == "editor"
	textOnly := multiline || d.kind == "input" || d.kind == "name" || d.kind == "session-rename"
	if d.kind == "cwd" && match("tui.input.tab") {
		return m.completeCwd(d)
	}
	if !textOnly {
		switch {
		case match("tui.select.up"):
			d.selected = max(0, d.selected-1)
			return nil
		case match("tui.select.down"):
			d.selected = min(max(0, len(options)-1), d.selected+1)
			return nil
		case match("tui.select.pageUp"):
			d.selected = max(0, d.selected-page)
			return nil
		case match("tui.select.pageDown"):
			d.selected = min(max(0, len(options)-1), d.selected+page)
			return nil
		}
	}
	if match("tui.select.confirm") || match("tui.input.submit") {
		return m.confirmDialog(t, d)
	}
	if d.kind == "confirm" || d.kind == "close" || d.kind == "resume" || d.kind == "session-delete" || d.kind == "session-send-confirm" || d.kind == "branch-loading" {
		return nil
	}
	for _, action := range m.cfg.Keys.Actions() {
		if (strings.HasPrefix(action, "tui.editor.") || action == "tui.input.newLine") && match(action) {
			if action == "tui.input.newLine" && !multiline {
				return nil
			}
			d.selected = 0
			return d.editor.Update(action, k)
		}
	}
	d.selected = 0
	return d.editor.Update("", k)
}
func (m *Model) confirmDialog(t *tab, d *dialog) (cmd tea.Cmd) {
	if cmd, handled := m.extensionInput(t, d); handled {
		return cmd
	}
	defer func() {
		if t == m.current() {
			cmd = tea.Batch(cmd, m.focus(t))
		}
	}()
	if d.kind == "cwd" {
		return m.finishCwd(t, d)
	}
	value := d.editor.Value()
	options := m.filtered(d)
	var selected option
	if len(options) > 0 {
		d.selected = min(d.selected, len(options)-1)
		selected = options[d.selected]
		value = selected.Value
	} else if d.kind != "input" && d.kind != "editor" && d.kind != "name" && d.kind != "session-rename" && d.kind != "cwd" {
		return nil
	}
	if cmd, handled := m.extensionOption(t, d, value); handled {
		return cmd
	}
	if d.kind == "session-rename" || d.kind == "session-delete" {
		return m.beginSessionAction(t, d, value)
	}
	if d.kind == "tree" {
		for _, b := range t.doc.Blocks {
			if b.ID == value || strings.HasPrefix(b.ID, value+"/") {
				t.offset = b.Start
				t.follow = false
				break
			}
		}
		t.dialogs = t.dialogs[1:]
		return nil
	}
	t.dialogs = t.dialogs[1:]
	switch d.kind {
	case "session-send-confirm":
		return m.confirmSessionSend(t, d, value)
	case "theme":
		m.restoreTheme(d)
		return m.chooseTheme(t, value)
	case "session":
		if strings.HasPrefix(value, "@") {
			var id int
			_, _ = fmt.Sscanf(value, "@%d", &id)
			for i, other := range m.tabs {
				if other.id == id {
					return m.activate(i)
				}
			}
			return nil
		}
		return m.resume(value)
	case "resume":
		return m.finishResume(t, value)
	case "model":
		model, ok := selected.Data.(session.ModelInfo)
		if !ok {
			return nil
		}
		return call(t, "set_model", map[string]any{"provider": model.Provider, "modelId": model.ID})
	case "thinking":
		return call(t, "set_thinking_level", map[string]any{"level": value})
	case "branch-loading":
		m.branchEpoch++
		t.originJump = nil
		return nil
	case "fork":
		return m.requestFork(t, value)
	case "name":
		return call(t, "set_session_name", map[string]any{"name": value})
	case "close":
		if value != "yes" {
			return nil
		}
		return m.closeSession(t)
	case "confirm":
		return respond(t, d, map[string]any{"confirmed": value == "yes"})
	default:
		return respond(t, d, map[string]any{"value": value})
	}
}

// Ctrl+D closes an idle tab directly; /close and in-flight work require consent.
func (m *Model) requestCloseSession(t *tab, confirm bool) tea.Cmd {
	if confirm || t.state.Busy || t.state.Compacting || t.sending || t.awaitingStart || t.resume.submit != nil {
		d := m.newDialog("close", "Close this session?", "The saved history stays on disk. Its running task will be stopped.")
		d.options = []option{{Label: "Cancel", Value: "no"}, {Label: "Close session", Value: "yes"}}
		t.dialogs = append(t.dialogs, d)
		return m.focus(t)
	}
	return m.closeSession(t)
}

// Remove only this tab. Pending openedMsg handlers close any client whose tab
// vanished during startup; the pool owns final shutdown of the last session.
func (m *Model) closeSession(t *tab) tea.Cmd {
	if t != nil && t.mutation != nil {
		t.status = "Session operation in progress."
		return nil
	}
	index := -1
	for i, other := range m.tabs {
		if other == t {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	client := t.client
	t.editor.Area.Blur()
	t.selection = transcriptSelection{}
	for _, d := range t.dialogs {
		d.editor.Area.Blur()
	}
	m.tabs = append(m.tabs[:index], m.tabs[index+1:]...)
	if len(m.tabs) == 0 {
		return tea.Quit
	}
	if index < m.active {
		m.active--
	}
	m.active = min(m.active, len(m.tabs)-1)
	focus := m.activate(m.active)
	return tea.Batch(focus, func() tea.Msg {
		if client != nil {
			_ = client.Close()
		}
		return nil
	})
}

func respond(t *tab, d *dialog, payload map[string]any) tea.Cmd {
	if d.requestID == "" || t.client == nil || t.dead {
		return nil
	}
	c, id := t.client, t.id
	payload["type"] = "extension_ui_response"
	payload["id"] = d.requestID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Send(ctx, payload); err != nil {
			return noticeMsg{id, err.Error()}
		}
		return nil
	}
}
func (m *Model) extension(t *tab, raw json.RawMessage) tea.Cmd {
	var e struct {
		ID, Method, Title, Message, DefaultValue, Prefill, Text, StatusKey, StatusText, WidgetKey, Placeholder string
		Options, WidgetLines                                                                                   []string
		Timeout                                                                                                int64
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		t.status = err.Error()
		return nil
	}
	switch e.Method {
	case "notify":
		if !t.extensionEvent(e.Message) {
			t.notify(e.Message)
		}
	case "setStatus":
		if e.StatusText == "" {
			delete(t.statuses, e.StatusKey)
		} else {
			t.statuses[e.StatusKey] = e.StatusText
		}
	case "setWidget":
		if e.WidgetLines == nil {
			delete(t.widgets, e.WidgetKey)
		} else {
			t.widgets[e.WidgetKey] = e.WidgetLines
		}
	case "setTitle":
		t.title = e.Title
	case "setWorkingMessage":
		t.status = e.Message
	case "set_editor_text":
		t.setValue(e.Text)
		m.resizeEditor(t)
	case "select", "confirm", "input", "editor":
		// Replaying a still-pending shared request must not replace a local
		// partial answer or stack the same question twice.
		if e.ID != "" {
			for _, existing := range t.dialogs {
				if existing.requestID == e.ID {
					return nil
				}
			}
		}
		d := m.newDialog(e.Method, e.Title, e.Message)
		d.requestID = e.ID
		if e.Timeout > 0 {
			d.deadline = time.Now().Add(time.Duration(e.Timeout) * time.Millisecond)
		}
		if e.Method == "confirm" {
			d.options = []option{{Label: "No", Value: "no"}, {Label: "Yes", Value: "yes"}}
		} else {
			for _, o := range e.Options {
				d.options = append(d.options, option{Label: o, Value: o})
			}
		}
		if e.Method == "editor" {
			d.editor.SetValue(e.Prefill)
		} else {
			d.editor.SetValue(e.DefaultValue)
		}
		d.editor.Area.Placeholder = e.Placeholder
		if e.Method == "input" {
			reason := m.openExtensionDialog(t, d, e.Title, e.Placeholder)
			if reason != "" {
				t.notify(reason)
				return respond(t, d, map[string]any{"cancelled": true})
			}
		}
		t.dialogs = append(t.dialogs, d)
		if t == m.current() {
			return m.focus(t)
		}
	}
	return nil
}
