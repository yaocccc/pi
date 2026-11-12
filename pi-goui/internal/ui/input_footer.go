package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	frames "pi-goui/internal/render/frame"
)

type editorFooter struct {
	text            string
	model, thinking hitRect
	visible         bool
}

func (m *Model) editorContentHeight(t *tab) int { return max(1, t.editor.Area.Height()) }
func (m *Model) editorFrameHeight(t *tab) int {
	return m.editorContentHeight(t) + 4
}
func (m *Model) editorTop(t *tab) int { return m.height - m.editorFrameHeight(t) }
func (m *Model) compactKey(action, preferred string) string {
	keys := m.cfg.Keys.Keys(action)
	if len(keys) == 0 {
		return "—"
	}
	key := keys[0]
	for _, candidate := range keys {
		if candidate == preferred {
			key = candidate
			break
		}
	}
	switch key {
	case "enter":
		return "↵"
	case "shift+enter":
		return "⇧↵"
	}
	return strings.ReplaceAll(key, "ctrl+", "^")
}

// One layout supplies both the styled footer and its exact terminal hit regions.
func (m *Model) inputFooter(t *tab) editorFooter {
	width := max(0, m.mainWidth()-4)
	footer := editorFooter{visible: m.height >= 8 && len(t.dialogs) == 0 && width > 0}
	if width == 0 {
		return footer
	}
	exit := "quit"
	if len(m.tabs) > 1 {
		exit = "close"
	}
	help := m.compactKey("tui.input.submit", "") + " send  " + m.compactKey("tui.input.newLine", "ctrl+j") + " 换行  " + m.compactKey("app.exit", "") + " " + exit
	model := t.state.Model.ID
	if model == "" {
		model = t.state.Model.Name
	}
	if model == "" {
		model = "select model"
	}
	model = strings.Join(strings.Fields(render.Clean(model)), " ")
	thinking := strings.Join(strings.Fields(render.Clean(t.state.Thinking)), " ")
	if thinking == "" {
		thinking = "off"
	}
	budget := max(1, width-min(24, width/2)-1)
	branch := ""
	if git := m.sidebarContext().Git; git != nil && budget >= 20 {
		name := strings.Join(strings.Fields(render.Clean(git.Branch)), " ")
		if name != "" {
			branch = ansi.Truncate(" "+name, min(24, budget/3), "…") + " · "
			budget -= ansi.StringWidth(branch)
		}
	}
	var right string
	if budget >= 7 {
		thinking = ansi.Truncate(thinking, min(8, max(1, budget/3)), "…")
		model = ansi.Truncate(model, max(1, budget-ansi.StringWidth(thinking)-3), "…")
		right = model + " · " + thinking
	} else {
		model = ansi.Truncate(model, budget, "…")
		thinking = ""
		right = model
	}
	right = branch + right
	x := width - ansi.StringWidth(right)
	help = ansi.Truncate(help, max(0, x-1), "…")
	padding := strings.Repeat(" ", max(0, x-ansi.StringWidth(help)))
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(m.cfg.Theme.Text))
	footer.text = m.paint(help+padding+branch, m.cfg.Theme.Muted) + style.Render(model)
	footer.model = hitRect{x: 2 + x + ansi.StringWidth(branch), y: m.height - 2, width: ansi.StringWidth(model)}
	if thinking != "" {
		footer.text += m.paint(" · ", m.cfg.Theme.Muted) + style.Render(thinking)
		footer.thinking = hitRect{x: footer.model.x + footer.model.width + 3, y: m.height - 2, width: ansi.StringWidth(thinking)}
	}
	return footer
}
func (m *Model) editorFrame(t *tab) string {
	width := m.mainWidth()
	inside := max(1, width-4)
	// Keep a dedicated spacer above the footer, independent of editable rows.
	body := fitBox(render.StyleImageReferences(t.editor.Area.View(), len(t.images)), inside, m.editorContentHeight(t)) + "\n\n" + m.inputFooter(t).text
	frame := strings.Join(frames.Box("input", strings.Split(body, "\n"), width, m.cfg.Theme), "\n")
	return fitBox(frame, width, m.editorFrameHeight(t))
}
