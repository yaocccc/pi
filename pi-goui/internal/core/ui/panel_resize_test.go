package ui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"pi-goui/internal/extensions/hostui"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func resizeBenchmarkModel(b *testing.B, kind string, count int) *Model {
	b.Helper()
	root := b.TempDir()
	cfg, err := config.Load(root)
	if err != nil {
		b.Fatal(err)
	}
	m := New(Options{Cwd: root, Bin: filepath.Join(root, "not-real-pi")}, cfg)
	b.Cleanup(func() { _ = m.Close() })
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	t := m.current()
	item := largeWorkerItem(count)
	if kind == "source" {
		args, _ := json.Marshal(map[string]any{"path": "/tmp/large.go", "content": strings.Repeat("var message = \"完整源码 👩‍💻 and a long line of text that must wrap correctly\"\n", count)})
		item = session.Item{ID: "source", Role: "tool", ToolName: "write", Arguments: args, HasResult: true}
	}
	t.state.Items = []session.Item{item}
	t.expanded = true
	m.document(t)
	m.openInspector(item.ID)
	if kind == "worker" {
		m.inspector.content = &hostui.Inspector{Key: "index:large:0"}
	}
	m.inspectorRows()
	m.View()
	return m
}

// Exercise actual update + full View, not only inspectorRows/cache lookup.
func BenchmarkPanelResize(b *testing.B) {
	for _, kind := range []string{"worker", "source"} {
		for _, count := range []int{200, 1000} {
			for _, phase := range []string{"motion-view", "gesture"} {
				b.Run(fmt.Sprintf("%s/%d/%s", kind, count, phase), func(b *testing.B) {
					m := resizeBenchmarkModel(b, kind, count)
					press := func() { m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2}) }
					move := func(width int) {
						m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - width, Y: 2})
						m.View()
					}
					if phase == "motion-view" {
						press()
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if phase == "motion-view" {
							move(45 + i%25)
							continue
						}
						press()
						for step := 0; step < 10; step++ {
							move(45 + step*3)
						}
						m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: m.width - 72, Y: 2})
						m.View()
					}
				})
			}
		}
	}
}

func dragPanel(t *testing.T, m *Model, width int) {
	t.Helper()
	x := m.mainWidth()
	if m.pointerShapeAt(x, 2) != "ew-resize" {
		t.Fatal("missing resize cursor")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: 2})
	if !m.panelResize.dragging {
		t.Fatal("resize not started")
	}
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: m.width - width, Y: 2})
	if cmd := m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: m.width - width, Y: 2}); cmd != nil {
		t.Fatal("resize copied text")
	}
	if m.panelResize.dragging || m.current().selection.dragging {
		t.Fatal("resize selection leak")
	}
}

func TestPanelResizeIndependentRuntimePreferences(t *testing.T) {
	m := inspectorFixture(t)
	dragPanel(t, m, 48)
	if m.sidebarWidth() != 48 {
		t.Fatal("sidebar width")
	}
	m.openInspector("write-1")
	if m.sidebarWidth() != min(60, m.width/3) {
		t.Fatal("inspector inherited sidebar width")
	}
	dragPanel(t, m, 80)
	if m.sidebarWidth() != 80 {
		t.Fatal("inspector width")
	}
	m.closeInspector()
	if m.sidebarWidth() != 48 {
		t.Fatal("sidebar preference lost")
	}
	m.openInspector("write-1")
	if m.sidebarWidth() != 80 {
		t.Fatal("inspector preference lost")
	}
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	if m.sidebarWidth() != 30 {
		t.Fatal("main viewport not protected")
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if m.sidebarWidth() != 80 {
		t.Fatal("resize overwrote preference")
	}
	dragPanel(t, m, 999)
	if m.mainWidth() != 40 {
		t.Fatal("maximum width")
	}
	dragPanel(t, m, 1)
	if m.sidebarWidth() != 20 {
		t.Fatal("minimum width")
	}
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 40})
	if m.panelResizeTarget(0, 2) {
		t.Fatal("fullscreen has resize handle")
	}
	fresh := inspectorFixture(t)
	if fresh.sidebarWidth() != 32 {
		t.Fatal("preference persisted")
	}
	fresh.openInspector("write-1")
	if fresh.sidebarWidth() != 53 {
		t.Fatal("detail preference persisted")
	}
}

func TestPanelResizeFreezesFullRenderingUntilRelease(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(fmt.Sprint(detail), func(t *testing.T) {
			m := inspectorFixture(t)
			tab := m.current()
			if detail {
				m.openInspector("write-1")
			}
			rows := m.inspectorRows()
			key := m.inspector.cache
			doc := tab.doc.Text
			editorWidth := tab.editor.Area.Width()
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2})
			// RPC state keeps updating; only the displayed snapshot is frozen.
			args, _ := json.Marshal(map[string]any{"path": "/tmp/new.go", "content": strings.Repeat("完整内容 👩‍💻 long code line must wrap not vanish\n", 200) + "FINAL_TAIL"})
			tab.state.Items[0].Arguments = args
			tab.dirty = true
			for _, width := range []int{30, 80, 45, 90, 60} {
				m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - width, Y: 2})
				for _, line := range strings.Split(m.View().Content, "\n") {
					if ansi.StringWidth(line) > m.width {
						t.Fatal("preview overflow")
					}
				}
				if tab.doc.Text != doc || tab.editor.Area.Width() != editorWidth {
					t.Fatal("motion reflowed transcript/editor")
				}
				if detail && (&m.inspector.rows[0] != &rows[0] || m.inspector.cache != key) {
					t.Fatal("motion regenerated full inspector rows")
				}
			}
			m.inspector.offset = 999999
			m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: m.width - 60, Y: 2})
			m.View()
			if m.panelResize.dragging || tab.editor.Area.Width() == editorWidth {
				t.Fatal("release did not reflow editor")
			}
			if tab.doc.Text == doc || tab.dirty {
				t.Fatal("release lost streamed transcript update")
			}
			if detail {
				want := render.ToolResultRows(tab.state.Items[0], m.cfg.Theme, 60-2*m.inspectorInset())
				if !reflect.DeepEqual(m.inspectorRows(), want) {
					t.Fatal("release not equivalent to full render")
				}
				if !strings.Contains(ansi.Strip(strings.Join(want, "\n")), "FINAL_TAIL") {
					t.Fatal("release lost tail")
				}
				if m.inspector.offset > max(0, len(want)-m.inspectorBodyHeight()) {
					t.Fatal("release left scroll past bottom")
				}
				before := &m.inspector.rows[0]
				m.View()
				if &m.inspector.rows[0] != before {
					t.Fatal("release failed to cache final reflow")
				}
			}
		})
	}
}

func TestPanelResizeCancellationReflows(t *testing.T) {
	for _, reason := range []string{"blur", "resize", "lost-button", "close", "switch", "removed"} {
		t.Run(reason, func(t *testing.T) {
			m := inspectorFixture(t)
			m.openInspector("write-1")
			m.View()
			m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2})
			m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - 80, Y: 2})
			switch reason {
			case "blur":
				m.Update(tea.BlurMsg{})
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			case "lost-button":
				m.Update(tea.MouseMotionMsg{Button: tea.MouseNone, X: m.width - 80, Y: 2})
			case "close":
				m.closeInspector()
			case "switch":
				m.addTab("")
				m.activate(1)
			case "removed":
				m.current().state.Items = nil
				m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - 80, Y: 2})
			}
			if m.panelResize.dragging {
				t.Fatal("stuck drag")
			}
			m.View()
			if m.inspectorOpen() && m.inspector.cache.width != m.sidebarWidth()-2*m.inspectorInset() {
				t.Fatal("cancel left stale wrapping")
			}
		})
	}
}

func TestPanelResizeBackgroundEditorDefersUntilActivation(t *testing.T) {
	m := inspectorFixture(t)
	first := m.current()
	other := m.addTab("")
	m.activate(0)
	oldWidth := other.editor.Area.Width()
	m.openInspector("write-1")
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2})
	for _, width := range []int{50, 70, 90} {
		m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: m.width - width, Y: 2})
		if other.editor.Area.Width() != oldWidth {
			t.Fatal("motion resized background editor")
		}
	}
	m.Update(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: m.width - 90, Y: 2})
	if first.dirty || !other.dirty {
		t.Fatal("release dirty flags incorrect")
	}
	m.activate(1)
	if other.editor.Area.Width() != m.mainWidth()-4 {
		t.Fatal("activation did not resize background editor")
	}
}

func TestPanelResizeDoesNotClaimBody(t *testing.T) {
	m := inspectorFixture(t)
	m.openInspector("write-1")
	if m.beginPanelResize(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth() + m.inspectorInset(), Y: 2}) {
		t.Fatal("claimed body selection")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: m.mainWidth(), Y: 2})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.panelResize.dragging {
		t.Fatal("window resize kept drag")
	}
}
