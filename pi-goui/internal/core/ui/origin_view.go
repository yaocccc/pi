package ui

import (
	"fmt"
	"path/filepath"
	"pi-goui/internal/core/render"
	"strings"
)

func (m *Model) chatTop(t *tab) int {
	if t != nil && t.origin.ID == t.state.ID && t.origin.ParentSession != "" && m.height >= 8 && !m.inspectorFullscreen() {
		return 1
	}
	return transcriptTop
}

func (m *Model) originTitle(t *tab) string {
	name := filepath.Base(t.origin.ParentSession)
	for _, s := range m.knownBranches() {
		if sameSessionPath(s.Path, t.origin.ParentSession) && (t.origin.Fork == nil || t.origin.Fork.SourceID == s.ID) {
			name = branchName(s)
			break
		}
	}
	label := "⑂ 来自「" + strings.Join(strings.Fields(render.Clean(name)), " ") + "」"
	if p := t.origin.Fork; p != nil {
		label += fmt.Sprintf(" · 第 %d 轮", p.Turn)
	}
	return label + "  [查看来源]"
}

func (m *Model) originHit(x, y int) bool {
	t := m.current()
	return t != nil && len(t.dialogs) == 0 && !t.selection.dragging && !m.panelResize.dragging && m.chatTop(t) > 0 && y == 0 && x >= 0 && x < m.mainWidth()
}
