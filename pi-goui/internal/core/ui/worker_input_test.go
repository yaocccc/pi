package ui

import (
	"fmt"
	"pi-goui/internal/extensions/hostui"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func inputList(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "- path-%02d.go\n", i)
	}
	return b.String()
}

func TestWorkerInputListInteractionAndCache(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m, parent, _ := workerPageFixture(t)
			receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
			// Match the actual panel body width used by mouse coordinates.
			m.width = width + 40 + 2*m.inspectorInset()
			m.panelResize.inspector = width + 2*m.inspectorInset()
			m.height = 60
			w := m.inspector.content.(*hostui.Inspector).Worker
			original := "# Files\n\n" + inputList(12) + "\n# Other files\n\n" + inputList(10) + "\n展开剩余 7 项\n\ue000 user-supplied marker"
			w.Visible = []session.Item{{ID: "input", Role: "user", Text: original, MessageIndex: 1}}
			w.Revision++
			rows := m.inspectorRows()
			if len(w.InputActions) != 2 {
				t.Fatalf("no action metadata: %+v\n%s", w.InputActions, strings.Join(rows, "\n"))
			}
			if strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "path-11.go") {
				t.Fatal("list not collapsed")
			}
			again := m.inspectorRows()
			if &rows[0] != &again[0] {
				t.Fatal("static hover rebuilt document")
			}
			a := w.InputActions[0]
			// Scroll to the action rather than relying on auto-follow position.
			m.inspector.followPaused = true
			m.inspector.offset = min(max(0, a.Rect.Y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			left, _ := m.inspectorBounds()
			x, y := left+m.inspectorInset()+a.Rect.X, 2+a.Rect.Y-m.inspector.offset
			if m.pointerShapeAt(x, y) != "pointer" {
				t.Fatal("no action pointer")
			}
			if _, ok := m.extensionInputTarget(x-1, y); ok {
				t.Fatal("padding became target")
			}
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if !w.InputExpanded[a.Key] || w.InputExpanded[w.InputActions[1].Key] {
				t.Fatal("lists not independently toggled")
			}
			if !strings.Contains(ansi.Strip(w.Doc.Text), "path-11.go") {
				t.Fatal("expanded content missing")
			}
			if w.Visible[0].Text != original {
				t.Fatal("mutated original")
			}
			// Width reflow does not lose the fold preference.
			m.extensionConversationRows(width + 1)
			if !w.InputExpanded[a.Key] {
				t.Fatal("reflow reset fold")
			}
			m.extensionConversationRows(width)
			a = w.InputActions[0]
			m.inspector.offset = min(max(0, a.Rect.Y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			x, y = left+m.inspectorInset()+a.Rect.X, 2+a.Rect.Y-m.inspector.offset
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorMotion(tea.Mouse{Button: tea.MouseLeft, X: x + 1, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x + 1, Y: y})
			if !w.InputExpanded[a.Key] {
				t.Fatal("drag selection toggled list")
			}
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if w.InputExpanded[a.Key] {
				t.Fatal("collapse failed")
			}
			m.Update(tea.WindowSizeMsg{Width: 30, Height: 40})
			m.inspectorRows()
			a = w.InputActions[0]
			m.inspector.offset = min(max(0, a.Rect.Y-3), max(0, len(m.inspector.rows)-m.inspectorBodyHeight()))
			x, y = m.inspectorInset()+a.Rect.X, 2+a.Rect.Y-m.inspector.offset
			m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRelease(tea.Mouse{Button: tea.MouseLeft, X: x, Y: y})
			m.inspectorRows()
			if !w.InputExpanded[a.Key] {
				t.Fatal("fullscreen action failed")
			}
			m.closeInspector()
			receiveWorkerCommand(t, m, m.openExtensionInspector(parent, "w", "index:batch:0"))
			if len(m.inspector.content.(*hostui.Inspector).Worker.InputExpanded) != 0 {
				t.Fatal("new task view inherited list folds")
			}
		})
	}
}
