// Package extension defines optional frontend contributions and their instance
// lifecycle. It contains no third-party protocol names or implementations.
package extension

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"encoding/json"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/input"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"time"
)

// Extension contributes optional capabilities. Each assembly calls Capabilities
// once; factories create independent state for each tab, dialog and working run.
type Extension interface{ Capabilities() Capabilities }

// Capabilities declares what a module implements; nil means unsupported.
// Dispatch follows slice order. Tool rendering, dialogs, inspectors, controls
// and event handlers select the first handler; observations and polling fan out.
// Returned controllers remain private to the extension, not copied into core.
type Capabilities struct {
	// Tools is immutable for the lifetime of one Runtime.
	Tools *render.ToolViews
	// Factories must return independent state, never a shared mutable singleton.
	NewProjection func() extensionapi.SessionProjection
	NewSession    func() Session
	// SessionMessageID routes asynchronous replies to the originating tab.
	SessionMessageID func(msg tea.Msg) (tabID int, handled bool)
	// A nil handler and empty error leave the native dialog untouched.
	// Publish view changes before synchronous Resize/Close callbacks.
	OpenDialog func(host DialogHost, view DialogView, title, message string) (handler DialogHandler, errorMessage string)
	// Inspectors must use the supplied combined Tools, including in child views.
	NewInspector          func(item session.Item, key string, showThinking bool, client Client, tools *render.ToolViews) (Inspector, bool)
	NewInspectorContent   func(item session.Item, key string, tools *render.ToolViews) Inspector
	IsInspectorMessage    func(msg tea.Msg) bool
	InspectableCollection func(item session.Item) bool
	// A fresh usage observer is created for each working run.
	NewUsage func() Usage
}

// Client is the optional backend bridge; implementations must tolerate its absence.
type Client interface {
	CallExtension(ctx context.Context, method string, params any) (json.RawMessage, error)
}
type Rect struct{ X, Y, Width, Height int }

func (r Rect) Contains(x, y int) bool {
	h := r.Height
	if h == 0 {
		h = 1
	}
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+h
}

type Controls struct {
	Hover bool
}
type ControlContext struct {
	Blocked, PointerKnown, Hover                                        bool
	SidebarWidth, MainWidth, Width, Height, ModeRow, PointerX, PointerY int
	Theme                                                               config.Theme
}

// ControlView exposes only geometry and interaction, not provider settings.
type ControlView interface {
	Geometry() (target Rect, popup Rect, available bool)
	Visible() bool
	Click(x, y int) (handled bool, choice string)
	Overlay(view string, backdrop func(string, int, int) string) string
}

// Session owns one tab's optional presentation. Sync/Update/Sidebar receive
// ordered broadcasts; ignore unrelated messages and preserve other providers'
// sidebar fields. Asynchronous replies must validate their own revision/owner.
type Session interface {
	Sync(tabID int, state session.State) tea.Cmd
	Update(msg tea.Msg, state session.State, dead bool, notify func(string))
	Sidebar(ctx *render.SidebarContext, state session.State, dead bool)
	Scroll(state session.State, layout render.SidebarLayout, y, delta int) (handled bool)
	// An empty choice polls; a nonempty choice comes only from this session's control.
	Request(tabID int, client Client, dead bool, choice string, now time.Time) tea.Cmd
	// Return nil when no control is available for this context.
	Control(ctx ControlContext) ControlView
	Event(message string, notify func(string)) (handled bool)
}

// Usage reports authoritative, non-overlapping counters. Runtime sums providers.
type Usage interface {
	Observe(event json.RawMessage)
	Totals() (inputTokens, outputTokens float64)
}
type ConversationTarget struct {
	ID   string
	Rect Rect
}
type InspectorView struct {
	Conversation  bool
	Title, Status string
	Document      render.Document
	Items         []session.Item
	Targets       []ConversationTarget
}

// Inspector owns extension-specific detail state. Core owns page lifetime,
// scrolling and selection; updates must reject replies belonging to another page.
type Inspector interface {
	Matches(item session.Item, key string) bool
	Resolve(item session.Item) (resolved session.Item, found bool)
	// Version advances whenever private state changes the rendered rows.
	Version() uint64
	Poll(item session.Item, now time.Time) tea.Cmd
	Update(msg tea.Msg, item *session.Item) (cmd tea.Cmd, handled bool)
	View() InspectorView
	Rows(item session.Item, theme config.Theme, width int, options func(session.State, int, bool, bool, map[string]bool) render.Options, inspectLabel string) []string
	Activity(row int) bool
	ToggleThinking()
	ToggleFold(row int)
	InputTarget(x, row int) (key string, found bool)
	ToggleInput(key string)
	// Children retain their parent's combined rendering provider.
	Child(id string) (itemID string, child Inspector, handled bool)
}

type Option struct {
	Label, Detail, Value string
	Data                 any
}
type DialogView struct {
	Kind, Title, Message string
	Options              []Option
	Selected             int
	Editor               *input.Editor
}
type DialogHost struct {
	// Change publishes a generic view before a synchronous host callback.
	Change  func(DialogView)
	Resize  func()
	Close   func(map[string]any) tea.Cmd
	Page    func() (start, total, visible int)
	Actions func() []string
	Match   func(action, key string) bool
}

// DialogHandler owns private form state. Only ordinary modal presentation is
// synchronized with the host; no backend or questionnaire records cross here.
type DialogHandler interface {
	View() DialogView
	SetView(DialogView)
	MultilineInput() bool
	Unfiltered() bool
	InputMode() (input bool, handled bool)
	FocusInput() bool
	HasLongMessage() bool
	WrapOptions() bool
	MessageRows(message string, width, height int) (rows []string, start, total int)
	Heading(fallback string, width int) string
	Guidance(fallback string, start, total, count int) string
	MultipleChoice() bool
	HandleKey(host DialogHost, key tea.KeyPressMsg) (cmd tea.Cmd, handled bool)
	Cancel(host DialogHost) (cmd tea.Cmd, handled bool)
	ConfirmInput(host DialogHost) (cmd tea.Cmd, handled bool)
	ConfirmOption(host DialogHost, value string) (cmd tea.Cmd, handled bool)
}
