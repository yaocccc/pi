package hostui

import (
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/internal/protocol"
	"time"
)

// Adapter contributes host-facing interfaces; private forms/replay remain here.
type Adapter struct{}

func (Adapter) Capabilities() extension.Capabilities {
	return extension.Capabilities{
		NewSession:       func() extension.Session { return &Session{} },
		SessionMessageID: SessionMessageID,
		NewUsage:         func() extension.Usage { return &Usage{} },
		OpenDialog: func(h extension.DialogHost, v extension.DialogView, title, payload string) (extension.DialogHandler, string) {
			if protocol.Dialog(title) == "" {
				return nil, ""
			}
			d := &Dialog{}
			d.SetView(v)
			err := boundDialogHost(d, h).OpenExtensionDialog(d, title, payload)
			return d, err
		},
		NewInspector: func(item session.Item, key string, thinking bool, client extension.Client, tools *render.ToolViews) (extension.Inspector, bool) {
			p, ok := NewInspector(item, key, thinking, client, tools)
			if !ok {
				return nil, false
			}
			return p, true
		},
		NewInspectorContent: func(item session.Item, key string, tools *render.ToolViews) extension.Inspector {
			if !InspectableCollection(item) {
				return nil
			}
			return &Inspector{Key: key, Tools: tools}
		},
		IsInspectorMessage:    IsInspectorMessage,
		InspectableCollection: InspectableCollection,
	}
}

var _ extension.Extension = Adapter{}
var _ extension.Session = (*Session)(nil)
var _ extension.DialogHandler = (*Dialog)(nil)
var _ extension.Inspector = (*Inspector)(nil)

func (s *Session) Request(id int, client extension.Client, dead bool, choice string, now time.Time) tea.Cmd {
	return s.Codex.Request(id, client, dead, choice, now)
}
func (s *Session) Control(c extension.ControlContext) extension.ControlView {
	if !s.Codex.State.Available() {
		return nil
	}
	return CodexView{State: s.Codex.State, Blocked: c.Blocked, PointerKnown: c.PointerKnown, Hover: c.Hover, SidebarWidth: c.SidebarWidth, MainWidth: c.MainWidth, Width: c.Width, Height: c.Height, ModeRow: c.ModeRow, PointerX: c.PointerX, PointerY: c.PointerY, Theme: c.Theme}
}
func (s *Session) Event(message string, notify func(string)) bool {
	return s.Codex.Event(message, notify)
}
func (d *Dialog) View() extension.DialogView {
	return extension.DialogView{Kind: d.Kind, Title: d.Title, Message: d.Message, Options: d.Options, Selected: d.Selected, Editor: d.Editor}
}
func (d *Dialog) SetView(v extension.DialogView) {
	d.Kind, d.Title, d.Message, d.Options, d.Selected, d.Editor = v.Kind, v.Title, v.Message, v.Options, v.Selected, v.Editor
}

// A callback may synchronously lay out or close the modal. Publish its updated
// generic view first, just as the host's ordinary modal handlers do.
func boundDialogHost(d *Dialog, h extension.DialogHost) DialogHost {
	sync := func() {
		if h.Change != nil {
			h.Change(d.View())
		}
	}
	return DialogHost{
		Resize: func() {
			sync()
			if h.Resize != nil {
				h.Resize()
			}
		},
		Close: func(v map[string]any) tea.Cmd {
			sync()
			if h.Close != nil {
				return h.Close(v)
			}
			return nil
		},
		Page: func() (int, int, int) {
			sync()
			if h.Page != nil {
				return h.Page()
			}
			return 0, 0, 0
		},
		Actions: h.Actions, Match: h.Match,
	}
}
func (d *Dialog) HandleKey(h extension.DialogHost, k tea.KeyPressMsg) (tea.Cmd, bool) {
	return boundDialogHost(d, h).HandleKey(d, k)
}
func (d *Dialog) Cancel(h extension.DialogHost) (tea.Cmd, bool) {
	return boundDialogHost(d, h).Cancel(d)
}
func (d *Dialog) ConfirmInput(h extension.DialogHost) (tea.Cmd, bool) {
	return boundDialogHost(d, h).ConfirmInput(d)
}
func (d *Dialog) ConfirmOption(h extension.DialogHost, v string) (tea.Cmd, bool) {
	return boundDialogHost(d, h).ConfirmOption(d, v)
}
