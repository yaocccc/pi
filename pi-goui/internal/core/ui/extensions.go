package ui

// This is the single composition point for optional extension presentation.
import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions"
	"time"
)

type extensionRuntime = extension.Runtime

func (m *Model) initExtensions(t *tab) {
	if m.extensions == nil {
		m.extensions = extension.New(extensions.New())
	}
	t.state.SetExtensionProjection(m.extensions.NewProjection())
	t.extensions = m.extensions.NewSessions()
	t.working.newExtensions = m.extensions.NewUsage
	t.working.extensions = m.extensions.NewUsage()
}

// Only generic modal state and host operations cross the adapter boundary.
func (m *Model) extensionDialog(t *tab, d *dialog) (extension.DialogHost, *extension.Dialog, func()) {
	v := &d.extension
	v.Kind, v.Title, v.Message, v.Options, v.Selected, v.Editor = d.kind, d.title, d.message, d.options, d.selected, &d.editor
	commit := func() {
		d.kind, d.title, d.message, d.options, d.selected = v.Kind, v.Title, v.Message, v.Options, v.Selected
	}
	h := extension.DialogHost{
		Change: func(state extension.DialogView) { v.DialogView = state; commit() },
		Resize: func() { commit(); m.resizeDialog(d) },
		Close: func(payload map[string]any) tea.Cmd {
			commit()
			t.dialogs = t.dialogs[1:]
			return tea.Batch(respond(t, d, payload), m.focus(t))
		},
		Page: func() (int, int, int) {
			commit()
			p := m.popupGeometry(d)
			return p.messageStart, p.messageTotal, len(p.message)
		},
		Actions: m.cfg.Keys.Actions, Match: m.cfg.Keys.Match,
	}
	return h, v, commit
}

func (m *Model) extensionControlView() extension.Control {
	t := m.current()
	if t == nil {
		return extension.Sessions(nil).Control(extension.ControlContext{})
	}
	return t.extensions.Control(extension.ControlContext{Blocked: t.dead || m.inspectorOpen() || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "", PointerKnown: m.sidebarHistory.pointerKnown, Hover: m.extensionControls.Hover, SidebarWidth: m.sidebarWidth(), MainWidth: m.mainWidth(), Width: m.width, Height: m.height, ModeRow: m.sidebarLayout().ModeRow, PointerX: m.sidebarHistory.pointerX, PointerY: m.sidebarHistory.pointerY, Theme: m.cfg.Theme})
}
func (m *Model) extensionPopupGeometry() (hitRect, footerPopupRect, bool) {
	t, b, ok := m.extensionControlView().Geometry()
	return hitRect{x: t.X, y: t.Y, width: t.Width}, footerPopupRect{x: b.X, y: b.Y, width: b.Width, height: b.Height}, ok
}
func (m *Model) extensionPopupVisible() bool { return m.extensionControlView().Visible() }
func (m *Model) extensionClickCommand(x, y int) (tea.Cmd, bool) {
	control := m.extensionControlView()
	handled, choice := control.Click(x, y)
	if choice != "" {
		m.extensionControls.Hover = false
	}
	if !handled || choice == "" {
		return nil, handled
	}
	t := m.current()
	if t == nil {
		return nil, true
	}
	var client extension.Client
	if t.client != nil {
		client = t.client
	}
	return control.Request(t.id, client, t.dead, choice, time.Now()), true
}
func (m *Model) overlayExtensions(view string) string {
	return m.extensionControlView().Overlay(view, popupBackdropSlice)
}

func (m *Model) openExtensionInspector(t *tab, itemID, key string) tea.Cmd {
	for _, item := range t.state.Items {
		if item.ID != itemID {
			continue
		}
		if m.inspector.tabID == t.id && m.inspector.itemID == itemID && m.inspector.content != nil && m.inspector.content.Matches(item, key) {
			return m.pollExtensionInspectors(time.Now())
		}
		var client extension.Client
		if t.client != nil {
			client = t.client
		}
		content, ok := m.extensions.NewInspector(item, key, t.showThinking, client)
		if !ok {
			return nil
		}
		m.finishPanelResize()
		m.inspector = toolInspector{tabID: t.id, itemID: itemID, content: content}
		t.footerHover.active = ""
		m.extensionControls.Hover = false
		m.resizeEditor(t)
		m.document(t)
		return m.pollExtensionInspectors(time.Now())
	}
	return nil
}
func (m *Model) pollExtensionInspectors(now time.Time) tea.Cmd {
	var cmds []tea.Cmd
	for page := &m.inspector; page != nil; page = page.parent {
		if page.content == nil {
			continue
		}
		item, ok := m.inspectorItem(page)
		if !ok {
			continue
		}
		wasConversation := page.view().Conversation
		cmd := page.content.Poll(item, now)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if wasConversation && !page.view().Conversation {
			page.cache = inspectorKey{}
			page.rows = nil
			page.selection = transcriptSelection{}
		}
	}
	return tea.Batch(cmds...)
}
func (m *Model) receiveInspectorMessage(msg tea.Msg) tea.Cmd {
	if !m.inspectorOpen() {
		return nil
	}
	for page := &m.inspector; page != nil; page = page.parent {
		if page.content == nil {
			continue
		}
		item, ok := m.inspectorItem(page)
		var ptr *session.Item
		if ok {
			ptr = &item
		}
		wasConversation := page.view().Conversation
		cmd, handled := page.content.Update(msg, ptr)
		if wasConversation && !page.view().Conversation {
			page.cache = inspectorKey{}
			page.rows = nil
			page.selection = transcriptSelection{}
		}
		if handled {
			return cmd
		}
	}
	return nil
}

type inspectorContent = extension.Inspector
type inspectorView = extension.InspectorView

func (p *toolInspector) view() inspectorView {
	if p.content == nil {
		return inspectorView{}
	}
	return p.content.View()
}

func (m *Model) extensionConversationRows(width int) []string {
	p := &m.inspector
	if p.rows == nil || p.cache.version != p.content.Version() || p.cache.width != width || p.cache.theme != m.cfg.Theme {
		item, _ := m.inspectedTool()
		p.rows = p.content.Rows(item, m.cfg.Theme, width, conversationOptions, toolInspectButton)
		p.cache.version = p.content.Version()
		p.cache.width = width
		p.cache.theme = m.cfg.Theme
		p.selection = transcriptSelection{}
	}
	bottom := max(0, len(p.rows)-m.inspectorBodyHeight())
	if !p.followPaused && !p.selection.dragging {
		p.offset = bottom
	} else {
		p.offset = min(max(0, p.offset), bottom)
	}
	return p.rows
}

func (m *Model) extensionInputTarget(x, y int) (string, bool) {
	if m.inspector.content == nil || y < 2 || y >= m.height-2 {
		return "", false
	}
	left, _ := m.inspectorBounds()
	return m.inspector.content.InputTarget(x-left-m.inspectorInset(), m.inspectorPoint(x, y).line)
}

func (t *tab) extensionEvent(message string) bool { return t.extensions.Event(message, t.notify) }

type dialogExtension = extension.Dialog

func (m *Model) extensionKey(t *tab, d *dialog, k tea.KeyPressMsg) (tea.Cmd, bool) {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return v.HandleKey(h, k)
}
func (m *Model) extensionCancel(t *tab, d *dialog) (tea.Cmd, bool) {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return v.Cancel(h)
}
func (m *Model) extensionInput(t *tab, d *dialog) (tea.Cmd, bool) {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return v.ConfirmInput(h)
}
func (m *Model) extensionOption(t *tab, d *dialog, value string) (tea.Cmd, bool) {
	h, v, done := m.extensionDialog(t, d)
	defer done()
	return v.ConfirmOption(h, value)
}

type option = extension.Option
type extensionSession = extension.Sessions
type extensionControls = extension.Controls
type extensionUsage = extension.Usages

func (t *tab) syncExtensions() tea.Cmd { return t.extensions.Sync(t.id, t.state) }
func (m *Model) extendSidebar(context *render.SidebarContext) {
	if t := m.current(); t != nil {
		t.extensions.Sidebar(context, t.state, t.dead)
	}
}
func (m *Model) scrollExtensions(t *tab, layout render.SidebarLayout, y, delta int) bool {
	return t.extensions.Scroll(t.state, layout, y, delta)
}
func (m *Model) extensionMessage(msg tea.Msg) (tea.Cmd, bool) {
	if m.extensions.IsInspectorMessage(msg) {
		return m.receiveInspectorMessage(msg), true
	}
	if id, ok := m.extensions.SessionMessageID(msg); ok {
		if t := m.byID(id); t != nil {
			t.extensions.Update(msg, t.state, t.dead, t.notify)
		}
		return nil, true
	}
	return nil, false
}
func (m *Model) pollExtensions(now time.Time) tea.Cmd {
	var cmd tea.Cmd
	if t := m.current(); t != nil {
		var client extension.Client
		if t.client != nil {
			client = t.client
		}
		cmd = t.extensions.Poll(t.id, client, t.dead, now)
	}
	return tea.Batch(m.pollExtensionInspectors(now), cmd)
}

func (m *Model) openExtensionDialog(t *tab, d *dialog, title, payload string) string {
	h, view, commit := m.extensionDialog(t, d)
	defer commit()
	return m.extensions.OpenDialog(view, h, title, payload)
}
