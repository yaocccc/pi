package ui

import (
	"encoding/json"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

type footerHoverState struct {
	kind             string
	active           string
	options          []option
	loading, setting bool
	offset           int
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
	if t == nil || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" || !m.sidebarHistory.pointerKnown {
		return ""
	}
	x, y := m.sidebarHistory.pointerX, m.sidebarHistory.pointerY
	if x < 0 || x >= m.mainWidth() || y < m.editorTop(t) || y >= m.height {
		box, _, _ := m.footerPopup(t, t.footerHover.active)
		if t.footerHover.active == "" || !box.contains(x, y) {
			return ""
		}
	}
	if kind := m.footerTargetKind(t); kind != "" {
		return kind
	}
	box, _, _ := m.footerPopup(t, t.footerHover.active)
	if x < box.x {
		return ""
	}
	return t.footerHover.active
}

func (m *Model) footerTargetKind(t *tab) string {
	if t == nil || len(t.dialogs) > 0 || t.selection.dragging || m.searching != "" || !m.sidebarHistory.pointerKnown {
		return ""
	}
	f := m.inputFooter(t)
	if !f.visible {
		return ""
	}
	x, y := m.sidebarHistory.pointerX, m.sidebarHistory.pointerY
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
	h.active = kind
	if h.setting || h.loading || h.kind == kind {
		return nil
	}
	*h = footerHoverState{kind: kind, active: kind}
	op := "get_available_models"
	if kind == "thinking" {
		op = "get_available_thinking_levels"
	}
	cmd := footerCall(t, kind, op, nil, false, option{})
	h.loading = cmd != nil
	if cmd == nil {
		*h = footerHoverState{}
	}
	return cmd
}

func (m *Model) clickFooterPopup(t *tab, x, y int) (tea.Cmd, bool) {
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
	op, data := "set_thinking_level", map[string]any{"level": o.Value}
	if model, ok := o.Data.(session.ModelInfo); ok {
		op, data = "set_model", map[string]any{"provider": model.Provider, "modelId": model.ID}
	}
	cmd := footerCall(t, kind, op, data, true, o)
	h.setting = cmd != nil
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
	if t == nil {
		return nil
	}
	h := &t.footerHover
	h.loading, h.setting = false, false
	if v.reply.err != nil {
		t.status = v.reply.err.Error()
		*h = footerHoverState{}
		return nil
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
	if v.kind == "model" {
		var data struct{ Models []session.ModelInfo }
		if err := json.Unmarshal(v.reply.data, &data); err != nil {
			t.status = err.Error()
			return nil
		}
		sort.SliceStable(data.Models, func(i, j int) bool {
			return data.Models[i].Provider < data.Models[j].Provider
		})
		for i, model := range data.Models {
			if i == 0 || model.Provider != data.Models[i-1].Provider {
				h.options = append(h.options, option{Label: model.Provider, Data: footerProviderHeading{}})
			}
			h.options = append(h.options, option{Label: model.ID, Value: model.Provider + "/" + model.ID, Data: model})
		}
	} else {
		var data struct{ Levels []string }
		if err := json.Unmarshal(v.reply.data, &data); err != nil {
			t.status = err.Error()
			return nil
		}
		for _, level := range data.Levels {
			h.options = append(h.options, option{Label: level, Value: level})
		}
	}
	if t == m.current() {
		return m.hoverFooter()
	}
	return nil
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
	width := 24
	if kind == "model" {
		for _, o := range t.footerHover.options {
			padding := 6
			if _, model := o.Data.(session.ModelInfo); model {
				padding += 2
			}
			width = max(width, ansi.StringWidth(render.Clean(o.Label))+padding)
		}
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
	count = min(count, max(1, len(t.footerHover.options)))
	start := min(t.footerHover.offset, max(0, len(t.footerHover.options)-count))
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
	rows := []string{"Loading…"}
	if h.kind == kind && !h.loading {
		rows = nil
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
			rows = []string{"No available options"}
		}
	}
	for len(rows) < count {
		rows = append(rows, "")
	}
	hint := "Click to set"
	if len(h.options) > count {
		hint = "Click · Scroll list"
	}
	box := render.SidebarDetailFrame(rows, []string{hint}, rect.width, m.cfg.Theme)
	// Apply hover styling after framing so the frame's text style cannot override it.
	hover := m.sidebarHistory.pointerY - rect.y - 1
	if h.kind == kind && !h.loading && rect.contains(m.sidebarHistory.pointerX, m.sidebarHistory.pointerY) && hover >= 0 && hover < count && start+hover < len(h.options) {
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
