package extension

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"time"
)

// Runtime is an immutable, ordered set of instance-bound contributions.
// First handled wins for choices and dispatch; observers run in list order.
type Runtime struct {
	capabilities []Capabilities
	Tools        *render.ToolViews
}

func New(extensions []Extension) *Runtime {
	r := &Runtime{}
	for _, e := range extensions {
		if e != nil {
			r.capabilities = append(r.capabilities, e.Capabilities())
		}
	}
	r.Tools = r.toolViews()
	return r
}
func (r *Runtime) NewSessions() Sessions {
	var out Sessions
	for _, c := range r.capabilities {
		if c.NewSession != nil {
			if s := c.NewSession(); s != nil {
				out = append(out, s)
			}
		}
	}
	return out
}
func (r *Runtime) NewUsage() Usages {
	var out Usages
	for _, c := range r.capabilities {
		if c.NewUsage != nil {
			if u := c.NewUsage(); u != nil {
				out = append(out, u)
			}
		}
	}
	return out
}
func (r *Runtime) SessionMessageID(msg tea.Msg) (int, bool) {
	for _, c := range r.capabilities {
		if c.SessionMessageID != nil {
			if id, ok := c.SessionMessageID(msg); ok {
				return id, true
			}
		}
	}
	return 0, false
}
func (r *Runtime) IsInspectorMessage(msg tea.Msg) bool {
	for _, c := range r.capabilities {
		if c.IsInspectorMessage != nil && c.IsInspectorMessage(msg) {
			return true
		}
	}
	return false
}
func (r *Runtime) InspectableCollection(item session.Item) bool {
	for _, c := range r.capabilities {
		if c.InspectableCollection != nil && c.InspectableCollection(item) {
			return true
		}
	}
	return false
}
func (r *Runtime) NewInspector(item session.Item, key string, thinking bool, client Client) (Inspector, bool) {
	for _, c := range r.capabilities {
		if c.NewInspector != nil {
			if p, ok := c.NewInspector(item, key, thinking, client, r.Tools); ok {
				return p, true
			}
		}
	}
	return nil, false
}
func (r *Runtime) NewInspectorContent(item session.Item, key string) Inspector {
	for _, c := range r.capabilities {
		if c.NewInspectorContent != nil {
			if p := c.NewInspectorContent(item, key, r.Tools); p != nil {
				return p
			}
		}
	}
	return nil
}
func (r *Runtime) OpenDialog(d *Dialog, h DialogHost, title, payload string) string {
	for _, c := range r.capabilities {
		if c.OpenDialog != nil {
			// Initial sizing must see the selected controller's presentation
			// methods, not the native fallback while its factory is still running.
			resized := false
			opening := h
			opening.Resize = func() { resized = true }
			handler, err := c.OpenDialog(opening, d.DialogView, title, payload)
			if handler != nil {
				d.Handler = handler
				d.DialogView = handler.View()
				if resized && h.Resize != nil {
					h.Resize()
				}
				return err
			}
			if err != "" {
				return err
			}
		}
	}
	return ""
}

type Sessions []Session

func (s Sessions) Sync(id int, state session.State) tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range s {
		cmds = append(cmds, v.Sync(id, state))
	}
	return tea.Batch(cmds...)
}
func (s Sessions) Update(msg tea.Msg, state session.State, dead bool, notify func(string)) {
	for _, v := range s {
		v.Update(msg, state, dead, notify)
	}
}
func (s Sessions) Sidebar(ctx *render.SidebarContext, state session.State, dead bool) {
	for _, v := range s {
		v.Sidebar(ctx, state, dead)
	}
}
func (s Sessions) Scroll(state session.State, l render.SidebarLayout, y, delta int) bool {
	for _, v := range s {
		if v.Scroll(state, l, y, delta) {
			return true
		}
	}
	return false
}

// Control binds the selected view to its owner. A click keeps that owner
// even if geometry, visibility or provider availability subsequently changes.
type Control struct {
	ControlView
	owner Session
}

func (c Control) Request(id int, client Client, dead bool, choice string, now time.Time) tea.Cmd {
	if c.owner == nil {
		return nil
	}
	return c.owner.Request(id, client, dead, choice, now)
}

func (s Sessions) Control(ctx ControlContext) Control {
	for _, v := range s {
		if view := v.Control(ctx); view != nil {
			return Control{ControlView: view, owner: v}
		}
	}
	return Control{ControlView: emptyControl{}}
}

// Poll broadcasts to all sessions; explicit choices use the selected Control.
func (s Sessions) Poll(id int, client Client, dead bool, now time.Time) tea.Cmd {
	var cmds []tea.Cmd
	for _, v := range s {
		cmds = append(cmds, v.Request(id, client, dead, "", now))
	}
	return tea.Batch(cmds...)
}
func (s Sessions) Event(message string, notify func(string)) bool {
	for _, v := range s {
		if v.Event(message, notify) {
			return true
		}
	}
	return false
}

type emptyControl struct{}

func (emptyControl) Geometry() (Rect, Rect, bool)                                { return Rect{}, Rect{}, false }
func (emptyControl) Visible() bool                                               { return false }
func (emptyControl) Click(int, int) (bool, string)                               { return false, "" }
func (emptyControl) Overlay(view string, _ func(string, int, int) string) string { return view }

type Usages []Usage

func (s Usages) Observe(raw json.RawMessage) {
	for _, v := range s {
		v.Observe(raw)
	}
}
func (s Usages) Totals() (float64, float64) {
	var in, out float64
	for _, v := range s {
		a, b := v.Totals()
		in += a
		out += b
	}
	return in, out
}

// Dialog is the host's neutral modal view plus an optional private controller.
// Its zero value behaves like an ordinary native modal.
type Dialog struct {
	DialogView
	Handler DialogHandler
}

func (d *Dialog) MultilineInput() bool { return d.Handler != nil && d.Handler.MultilineInput() }
func (d *Dialog) Unfiltered() bool     { return d.Handler != nil && d.Handler.Unfiltered() }
func (d *Dialog) InputMode() (bool, bool) {
	if d.Handler != nil {
		return d.Handler.InputMode()
	}
	return false, false
}
func (d *Dialog) FocusInput() bool     { return d.Handler == nil || d.Handler.FocusInput() }
func (d *Dialog) HasLongMessage() bool { return d.Handler != nil && d.Handler.HasLongMessage() }
func (d *Dialog) WrapOptions() bool    { return d.Handler != nil && d.Handler.WrapOptions() }
func (d *Dialog) MessageRows(s string, w, h int) ([]string, int, int) {
	if d.Handler != nil {
		return d.Handler.MessageRows(s, w, h)
	}
	return nil, 0, 0
}
func (d *Dialog) Heading(s string, w int) string {
	if d.Handler != nil {
		return d.Handler.Heading(s, w)
	}
	return s
}
func (d *Dialog) Guidance(s string, a, b, c int) string {
	if d.Handler != nil {
		return d.Handler.Guidance(s, a, b, c)
	}
	return s
}
func (d *Dialog) MultipleChoice() bool { return d.Handler != nil && d.Handler.MultipleChoice() }
func (d *Dialog) invoke(f func(DialogHandler) (tea.Cmd, bool)) (tea.Cmd, bool) {
	if d.Handler == nil {
		return nil, false
	}
	d.Handler.SetView(d.DialogView)
	cmd, ok := f(d.Handler)
	d.DialogView = d.Handler.View()
	return cmd, ok
}
func (d *Dialog) HandleKey(h DialogHost, k tea.KeyPressMsg) (tea.Cmd, bool) {
	return d.invoke(func(v DialogHandler) (tea.Cmd, bool) { return v.HandleKey(h, k) })
}
func (d *Dialog) Cancel(h DialogHost) (tea.Cmd, bool) {
	return d.invoke(func(v DialogHandler) (tea.Cmd, bool) { return v.Cancel(h) })
}
func (d *Dialog) ConfirmInput(h DialogHost) (tea.Cmd, bool) {
	return d.invoke(func(v DialogHandler) (tea.Cmd, bool) { return v.ConfirmInput(h) })
}
func (d *Dialog) ConfirmOption(h DialogHost, s string) (tea.Cmd, bool) {
	return d.invoke(func(v DialogHandler) (tea.Cmd, bool) { return v.ConfirmOption(h, s) })
}
