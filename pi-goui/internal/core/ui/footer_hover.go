package ui

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

type footerHoverState struct {
	kind             string
	active           string
	options          []option
	loading, setting bool
	failed, local    bool
	offset, width    int
}

// Provider headings occupy list rows but cannot be selected.
type footerProviderHeading struct{}

type footerReplyMsg struct {
	reply    replyMsg
	kind     string
	setting  bool
	selected option
}

func (m *Model) footerHoverKind(t *tab) string {
	if m.inspectorFullscreen() {
		return ""
	}
	if t == nil || t.readOnly() || t.cycle.phase == cycleClosing || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" || !m.sidebarHistory.pointerKnown {
		return ""
	}
	x, y := m.sidebarHistory.pointerX, m.sidebarHistory.pointerY
	if x < 0 || x >= m.mainWidth() || y < m.editorTop(t) || y >= m.height {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if t.footerHover.active == "" || !box.contains(x, y) {
			return ""
		}
	}
	if kind := m.footerTargetKind(t, x, y); kind != "" {
		return kind
	}
	box, _, _ := m.footerPopup(t, t.footerHover.active)
	if x < box.x {
		return ""
	}
	return t.footerHover.active
}

func (m *Model) footerTargetKind(t *tab, x, y int) string {
	if m.inspectorFullscreen() {
		return ""
	}
	if y != m.height-2 || t == nil || t.readOnly() || t.cycle.phase == cycleClosing || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" {
		return ""
	}
	f := m.inputFooter(t)
	if !f.visible {
		return ""
	}
	if f.model.contains(x, y) {
		return "model"
	}
	if f.thinking.contains(x, y) {
		return "thinking"
	}
	return ""
}

func footerCall(t *tab, kind, op string, data map[string]any, setting bool, selected option) tea.Cmd {
	cmd := call(t, op, data)
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return footerReplyMsg{cmd().(replyMsg), kind, setting, selected} }
}

func (m *Model) hoverFooter() tea.Cmd {
	t := m.current()
	kind := m.footerHoverKind(t)
	if kind == "" {
		if t != nil {
			t.footerHover.active = ""
		}
		return nil
	}
	h := &t.footerHover
	reenter := h.active != kind
	h.active = kind
	if h.setting || h.loading || (h.kind == kind && (!h.local || t.client == nil) && !(h.failed && reenter)) {
		return nil
	}
	*h = footerHoverState{kind: kind, active: kind}
	if t.client == nil {
		h.local = true
		h.options, h.width = t.footerCatalog.options(kind, &t.state)
		return nil
	}
	cmd := footerCall(t, kind, footerListOp(kind), nil, false, option{})
	h.loading = cmd != nil
	if cmd == nil {
		// No backend to query. Keep the popup reachable, without claiming
		// to have loaded this kind or starting a process on pointer motion.
		*h = footerHoverState{active: kind}
	}
	return cmd
}

func footerListOp(kind string) string {
	if kind == "thinking" {
		return "get_available_thinking_levels"
	}
	return "get_available_models"
}

func footerSetCall(t *tab, kind string, o option) tea.Cmd {
	if model, ok := o.Data.(session.ModelInfo); ok {
		return footerCall(t, kind, "set_model", map[string]any{"provider": model.Provider, "modelId": model.ID}, true, o)
	}
	return footerCall(t, kind, "set_thinking_level", map[string]any{"level": o.Value}, true, o)
}

func (m *Model) clickFooterPopup(t *tab, x, y int) (tea.Cmd, bool) {
	if t == nil || t.readOnly() || t.cycle.phase == cycleClosing {
		return nil, false
	}
	// Hovering is a read-only local gesture. An explicit click on an idle
	// model/thinking control may acquire the session to open its native menu.
	if t.client == nil {
		switch m.footerTargetKind(t, x, y) {
		case "model":
			return m.requestCall(t, "get_available_models", nil), true
		case "thinking":
			return m.requestCall(t, "get_available_thinking_levels", nil), true
		}
	}
	kind := m.footerHoverKind(t)
	if kind == "" {
		return nil, false
	}
	box, start, count := m.footerPopup(t, kind)
	if !box.contains(x, y) {
		return nil, false
	}
	h := &t.footerHover
	row := y - box.y - 1
	if h.loading || h.setting || h.kind != kind || row < 0 || row >= count || start+row >= len(h.options) {
		return nil, true
	}
	o := h.options[start+row]
	if _, heading := o.Data.(footerProviderHeading); heading {
		return nil, true
	}
	var cmd tea.Cmd
	if h.local || t.client == nil || t.resume.pending {
		// A cached ID is not evidence of availability for this Pi or account.
		cmd = m.beginCycle(t, &writeIntent{run: func() tea.Cmd { return footerCall(t, kind, footerListOp(kind), nil, false, o) }})
	} else {
		cmd = footerSetCall(t, kind, o)
	}
	h.setting = cmd != nil
	if cmd != nil {
		h.active = ""
	}
	return cmd, true
}

func (m *Model) scrollFooterPopup(t *tab, delta int) bool {
	kind := m.footerHoverKind(t)
	if kind == "" {
		return false
	}
	box, _, count := m.footerPopup(t, kind)
	if !box.contains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY) {
		return false
	}
	t.footerHover.offset = min(max(0, t.footerHover.offset+delta), max(0, len(t.footerHover.options)-count))
	return true
}

func (m *Model) footerReply(v footerReplyMsg) tea.Cmd {
	t := m.byID(v.reply.id)
	if t == nil || (v.reply.client != nil && (t.client != v.reply.client || t.cycle.generation != v.reply.generation)) {
		return nil
	}
	if v.reply.client != nil {
		t.cycle.inflight = max(0, t.cycle.inflight-1)
	}
	h := &t.footerHover
	h.loading, h.setting = false, false
	fail := func(err error) tea.Cmd {
		t.status = err.Error()
		*h = footerHoverState{kind: v.kind, active: h.active, failed: true}
		// If the pointer moved to the other control during this request,
		// service it even when the first lookup failed.
		if t == m.current() && m.footerHoverKind(t) != v.kind {
			return m.hoverFooter()
		}
		return nil
	}
	if v.reply.err != nil {
		return fail(v.reply.err)
	}
	if v.setting {
		if model, ok := v.selected.Data.(session.ModelInfo); ok {
			t.state.Model = model
		} else {
			t.state.Thinking = v.selected.Value
		}
		if t == m.current() {
			return tea.Batch(m.refresh(t), m.hoverFooter())
		}
		return m.refresh(t)
	}
	*h = footerHoverState{kind: v.kind, active: h.active}
	if v.kind == "model" {
		var data struct{ Models []session.ModelInfo }
		if err := json.Unmarshal(v.reply.data, &data); err != nil {
			return fail(err)
		}
		if data.Models == nil {
			return fail(errors.New("backend did not return a model list"))
		}
		h.options, h.width = footerModelOptions(data.Models)
	} else {
		var data struct{ Levels []string }
		if err := json.Unmarshal(v.reply.data, &data); err != nil {
			return fail(err)
		}
		if data.Levels == nil {
			return fail(errors.New("backend did not return thinking levels"))
		}
		for _, level := range data.Levels {
			h.options = append(h.options, option{Label: level, Value: level})
		}
	}
	if v.selected.Value != "" { // Explicit selection from a disconnected preview.
		for _, o := range h.options {
			if o.Value != v.selected.Value {
				continue
			}
			if model, ok := v.selected.Data.(session.ModelInfo); ok {
				live, ok := o.Data.(session.ModelInfo)
				if !ok || live.Provider != model.Provider || live.ID != model.ID {
					continue
				}
			}
			cmd := footerSetCall(t, v.kind, o)
			h.setting = cmd != nil
			return cmd
		}
		t.notify("Option no longer available; choose from the current Pi list")
		return m.reply(t, v.reply)
	}
	if t == m.current() {
		return m.hoverFooter()
	}
	return nil
}

func footerModelOptions(models []session.ModelInfo) ([]option, int) {
	sort.SliceStable(models, func(i, j int) bool { return models[i].Provider < models[j].Provider })
	var options []option
	var width int
	for i, model := range models {
		if i == 0 || model.Provider != models[i-1].Provider {
			options = append(options, option{Label: model.Provider, Data: footerProviderHeading{}})
			width = max(width, ansi.StringWidth(render.Clean(model.Provider))+6)
		}
		options = append(options, option{Label: model.ID, Value: model.Provider + "/" + model.ID, Data: model})
		width = max(width, ansi.StringWidth(render.Clean(model.ID))+8)
	}
	return options, width
}

type footerPopupRect struct{ x, y, width, height int }

func (r footerPopupRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

// Share popup geometry between drawing and pointer hit testing.
func (m *Model) footerPopup(t *tab, kind string) (footerPopupRect, int, int) {
	if t == nil || kind == "" {
		return footerPopupRect{}, 0, 0
	}
	h := &t.footerHover
	options := h.options
	if h.kind != kind || h.loading || h.setting {
		options = nil
	}
	width := 24
	if len(options) != 0 {
		width = max(width, h.width)
	}
	width = min(width, 44, m.mainWidth())
	f := m.inputFooter(t)
	anchor := f.model
	if kind == "thinking" {
		anchor = f.thinking
	}
	count := min(8, anchor.y-4)
	if width < 12 || count < 1 {
		return footerPopupRect{}, 0, 0
	}
	count = min(count, max(1, len(options)))
	start := min(h.offset, max(0, len(options)-count))
	height := count + 4
	return footerPopupRect{x: min(anchor.x, max(0, m.mainWidth()-width)), y: anchor.y - height, width: width, height: height}, start, count
}

func (m *Model) overlayFooterHover(t *tab, view string) string {
	kind := m.footerHoverKind(t)
	if kind == "" {
		return view
	}
	rect, start, count := m.footerPopup(t, kind)
	if rect.width == 0 {
		return view
	}
	h := t.footerHover
	var rows []string
	hint := "Click to set"
	switch {
	case h.loading:
		rows, hint = []string{"Loading…"}, ""
	case h.setting:
		rows, hint = []string{"Applying selection…"}, ""
	case t.cycle.phase == cycleStarting:
		rows, hint = []string{"Opening session…"}, ""
	case h.kind != kind || (h.local && len(h.options) == 0):
		label := "Model options"
		if kind == "thinking" {
			label = "Thinking levels"
		}
		rows, hint = []string{label}, "Click label to open"
		if t.client != nil {
			hint = "Hover label to load"
		}
	case h.failed:
		rows, hint = []string{"Options unavailable"}, "Re-hover to retry"
	default:
		if len(h.options) > count {
			hint = "Click · Scroll list"
		}
		if h.local {
			hint = "Local · click to check"
		}
		for _, o := range h.options[start:min(len(h.options), start+count)] {
			current := kind == "thinking" && o.Value == t.state.Thinking
			if model, ok := o.Data.(session.ModelInfo); ok {
				current = model.ID == t.state.Model.ID && model.Provider == t.state.Model.Provider
			}
			prefix := "  "
			if current {
				prefix = "› "
			}
			if _, heading := o.Data.(footerProviderHeading); heading {
				prefix = ""
			} else if kind == "model" {
				prefix = "  " + prefix
			}
			rows = append(rows, prefix+ansi.Truncate(render.Clean(o.Label), rect.width-4-ansi.StringWidth(prefix), "…"))
		}
		if len(rows) == 0 {
			rows, hint = []string{"No available options"}, ""
		}
	}
	for len(rows) < count {
		rows = append(rows, "")
	}
	box := render.SidebarDetailFrame(rows, []string{hint}, rect.width, m.cfg.Theme)
	// Apply hover styling after framing so the frame's text style cannot override it.
	hover := m.sidebarHistory.pointerY - rect.y - 1
	if h.kind == kind && !h.loading && !h.setting && rect.contains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY) && hover >= 0 && hover < count && start+hover < len(h.options) {
		if _, heading := h.options[start+hover].Data.(footerProviderHeading); !heading {
			line := box[hover+1]
			inner := ansi.Strip(ansi.Cut(line, 1, rect.width-1))
			box[hover+1] = ansi.Cut(line, 0, 1) + render.SidebarHoverText(inner) + ansi.Cut(line, rect.width-1, rect.width)
		}
	}
	lines := strings.Split(view, "\n")
	for i, line := range box {
		if rect.y+i >= 0 && rect.y+i < len(lines) {
			lines[rect.y+i] = popupBackdropSlice(lines[rect.y+i], 0, rect.x) + line + popupBackdropSlice(lines[rect.y+i], rect.x+rect.width, m.mainWidth())
		}
	}
	return strings.Join(lines, "\n")
}
