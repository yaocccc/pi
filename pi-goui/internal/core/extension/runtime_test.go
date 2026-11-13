package extension

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"reflect"
	"testing"
	"time"
)

type contribution struct{ capabilities Capabilities }

func (c contribution) Capabilities() Capabilities { return c.capabilities }

type usageCounter struct{ count int }

func (u *usageCounter) Observe(json.RawMessage)    { u.count++ }
func (u *usageCounter) Totals() (float64, float64) { return float64(u.count), 2 * float64(u.count) }

type sessionObserver struct {
	Session
	updates int
}

func (s *sessionObserver) Update(tea.Msg, session.State, bool, func(string)) { s.updates++ }

type inspectorFixture struct {
	Inspector
	tools *render.ToolViews
}
type modalFixture struct {
	DialogHandler
	view DialogView
}

func (m *modalFixture) View() DialogView     { return m.view }
func (m *modalFixture) SetView(v DialogView) { m.view = v }
func (m *modalFixture) ConfirmOption(h DialogHost, value string) (tea.Cmd, bool) {
	m.view.Title = value
	if h.Change != nil {
		h.Change(m.view)
	}
	return nil, true
}

type controlSession struct {
	Session
	view     ControlView
	width    int
	requests []string
}

func (s *controlSession) Control(ctx ControlContext) ControlView {
	if s.width != 0 && s.width != ctx.Width {
		return nil
	}
	return s.view
}
func (s *controlSession) Request(_ int, _ Client, _ bool, choice string, _ time.Time) tea.Cmd {
	s.requests = append(s.requests, choice)
	return nil
}

func TestSessionPollingBroadcastsButChoicesStayWithControlOwner(t *testing.T) {
	first, second, third := &controlSession{}, &controlSession{view: emptyControl{}, width: 80}, &controlSession{view: emptyControl{}}
	sessions := Sessions{first, second, third}
	sessions.Poll(1, nil, false, time.Time{})
	control := sessions.Control(ControlContext{Width: 80})
	second.view = nil // The clicked provider remains the owner after a layout change.
	control.Request(1, nil, false, "chosen", time.Time{})
	if !reflect.DeepEqual(first.requests, []string{""}) || !reflect.DeepEqual(second.requests, []string{"", "chosen"}) || !reflect.DeepEqual(third.requests, []string{""}) {
		t.Fatal("polling or chosen control ownership changed")
	}
}

func TestEmptyRuntimeRetainsNativeFallback(t *testing.T) {
	r := New(nil)
	if r.Tools != nil || r.NewProjection() != nil || len(r.NewSessions()) != 0 || len(r.NewUsage()) != 0 {
		t.Fatal("empty list enabled optional behavior")
	}
	if _, ok := r.NewInspector(session.Item{}, "", false, nil); ok {
		t.Fatal("unexpected inspector")
	}
	if r.NewInspectorContent(session.Item{}, "") != nil || r.IsInspectorMessage("message") || r.InspectableCollection(session.Item{}) {
		t.Fatal("unexpected recognition")
	}
	if _, ok := r.SessionMessageID("message"); ok {
		t.Fatal("unexpected owner")
	}
	d := Dialog{DialogView: DialogView{Title: "native"}}
	if err := r.OpenDialog(&d, DialogHost{}, "native", ""); err != "" || d.Handler != nil || d.Title != "native" {
		t.Fatal("native dialog replaced")
	}
	if d.Unfiltered() || d.MultilineInput() || !d.FocusInput() || d.HasLongMessage() || d.WrapOptions() || d.MultipleChoice() {
		t.Fatal("zero dialog lost native behavior")
	}
	if d.Heading("heading", 10) != "heading" || d.Guidance("help", 0, 0, 0) != "help" {
		t.Fatal("native help replaced")
	}
	if _, ok := d.HandleKey(DialogHost{}, tea.KeyPressMsg{}); ok {
		t.Fatal("native key consumed")
	}
	if view := r.NewSessions().Control(ControlContext{}); view.Visible() || view.Overlay("native", nil) != "native" {
		t.Fatal("native control replaced")
	}
	// Core renderers still produce native and generic documents without hooks.
	for _, name := range []string{"read", "codemode", "third-party"} {
		item := session.Item{ID: name, Role: "tool", ToolName: name, HasResult: true, Text: "native result"}
		if rows := render.ToolResultRows(item, config.Theme{}, 60, r.Tools); len(rows) == 0 {
			t.Fatalf("%s lost fallback", name)
		}
	}
}

func TestDialogBindsControllerBeforeInitialResize(t *testing.T) {
	r := New([]Extension{contribution{Capabilities{OpenDialog: func(h DialogHost, v DialogView, _, _ string) (DialogHandler, string) {
		h.Resize()
		v.Title = "initialized"
		return &modalFixture{view: v}, ""
	}}}})
	d := Dialog{}
	resized := false
	r.OpenDialog(&d, DialogHost{Resize: func() {
		resized = true
		if d.Handler == nil || d.Title != "initialized" {
			t.Fatal("initial layout ran before controller binding")
		}
	}}, "", "")
	if !resized {
		t.Fatal("initial layout was dropped")
	}
}

func TestOrderedToolCapabilitiesAndCacheIdentity(t *testing.T) {
	var calls []string
	provider := func(name string, handles bool) *render.ToolViews {
		return &render.ToolViews{
			Policy: func(string) extensionapi.CardPolicy {
				calls = append(calls, name+"/policy")
				return extensionapi.CardPolicy{Specialized: handles}
			},
			Layout: func(extensionapi.CardHost, session.Item, config.Theme, extensionapi.CardOptions, int) ([]string, []extensionapi.Block, bool) {
				calls = append(calls, name+"/layout")
				return []string{name}, nil, handles
			},
			Detail: func(extensionapi.CardHost, session.Item, config.Theme, int) ([]string, bool) {
				calls = append(calls, name+"/detail")
				return []string{name}, handles
			},
			FullDetail: func(extensionapi.DetailHost, string, session.Item, map[string]any, map[string]any) bool {
				calls = append(calls, name+"/full")
				return handles
			},
		}
	}
	list := []Extension{contribution{Capabilities{Tools: provider("first", false)}}, nil, contribution{Capabilities{Tools: provider("second", true)}}, contribution{Capabilities{Tools: provider("third", true)}}}
	a, b := New(list), New(list)
	if a.Tools == b.Tools {
		t.Fatal("runtime identity must not be shared even with the same contributed hooks")
	}
	if !a.Tools.Policy("tool").Specialized {
		t.Fatal("later policy was ignored")
	}
	rows, _, ok := a.Tools.Layout(extensionapi.CardHost{}, session.Item{}, config.Theme{}, extensionapi.CardOptions{}, 50)
	if !ok || rows[0] != "second" {
		t.Fatal("layout did not reach second provider")
	}
	rows, ok = a.Tools.Detail(extensionapi.CardHost{}, session.Item{}, config.Theme{}, 50)
	if !ok || rows[0] != "second" || !a.Tools.FullDetail(extensionapi.DetailHost{}, "tool", session.Item{}, nil, nil) {
		t.Fatal("detail did not reach second provider")
	}
	want := []string{"first/policy", "second/policy", "first/layout", "second/layout", "first/detail", "second/detail", "first/full", "second/full"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("dispatch order: %v", calls)
	}
}

func TestOrderedFactoriesDispatchAndInstanceIsolation(t *testing.T) {
	var order []string
	makeExtension := func(name string, handles bool) Extension {
		return contribution{Capabilities{
			NewSession:            func() Session { return &sessionObserver{} },
			NewUsage:              func() Usage { return &usageCounter{} },
			SessionMessageID:      func(tea.Msg) (int, bool) { order = append(order, name+"/owner"); return 42, handles },
			IsInspectorMessage:    func(tea.Msg) bool { return handles },
			InspectableCollection: func(session.Item) bool { return handles },
			OpenDialog: func(_ DialogHost, v DialogView, _, _ string) (DialogHandler, string) {
				order = append(order, name+"/dialog")
				if !handles {
					return nil, ""
				}
				v.Title = name
				return &modalFixture{view: v}, ""
			},
			NewInspector: func(_ session.Item, _ string, _ bool, _ Client, tools *render.ToolViews) (Inspector, bool) {
				order = append(order, name+"/inspector")
				if !handles {
					return nil, false
				}
				return &inspectorFixture{tools: tools}, true
			},
			NewInspectorContent: func(_ session.Item, _ string, tools *render.ToolViews) Inspector {
				order = append(order, name+"/content")
				if !handles {
					return nil
				}
				return &inspectorFixture{tools: tools}
			},
		}}
	}
	r := New([]Extension{makeExtension("first", false), makeExtension("second", true), makeExtension("third", true)})
	a, b := r.NewSessions(), r.NewSessions()
	a.Update("msg", session.State{}, false, nil)
	for i := range a {
		if a[i] == b[i] || a[i].(*sessionObserver).updates != 1 || b[i].(*sessionObserver).updates != 0 {
			t.Fatal("session state shared or observer omitted")
		}
	}
	x, y := r.NewUsage(), r.NewUsage()
	x.Observe(nil)
	if in, out := x.Totals(); in != 3 || out != 6 {
		t.Fatalf("usage contributions missing: %v %v", in, out)
	}
	if in, out := y.Totals(); in != 0 || out != 0 {
		t.Fatal("usage factories shared state")
	}
	if id, ok := r.SessionMessageID("msg"); !ok || id != 42 {
		t.Fatal("owner dispatch stopped at first refusal")
	}
	d := Dialog{}
	if err := r.OpenDialog(&d, DialogHost{}, "form", ""); err != "" || d.Title != "second" {
		t.Fatal("dialog dispatch order")
	}
	d.Title = "host changed"
	var published string
	if _, ok := d.ConfirmOption(DialogHost{Change: func(v DialogView) { published = v.Title }}, "saved"); !ok || d.Title != "saved" || published != "saved" {
		t.Fatal("modal synchronization lost")
	}
	p, ok := r.NewInspector(session.Item{}, "key", false, nil)
	if !ok || p.(*inspectorFixture).tools != r.Tools {
		t.Fatal("inspector tools lost")
	}
	if p = r.NewInspectorContent(session.Item{}, "key"); p == nil || p.(*inspectorFixture).tools != r.Tools {
		t.Fatal("content tools lost")
	}
	if !r.IsInspectorMessage("msg") || !r.InspectableCollection(session.Item{}) {
		t.Fatal("later recognizer ignored")
	}
	want := []string{"first/owner", "second/owner", "first/dialog", "second/dialog", "first/inspector", "second/inspector", "first/content", "second/content"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("capability dispatch order: %v", order)
	}
}
