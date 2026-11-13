package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/extensions/codexfast"
	"pi-goui/internal/session"
)

func inspectorFixture(t *testing.T) *Model {
	t.Helper()
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	var source []string
	for i := 1; i <= 35; i++ {
		source = append(source, fmt.Sprintf("line-%02d %s END-%02d", i, strings.Repeat("中文long", 25), i))
	}
	args, _ := json.Marshal(map[string]string{"path": "/tmp/inspect.go", "content": strings.Join(source, "\n")})
	tab := m.current()
	tab.state.Items = []session.Item{{ID: "write-1", Role: "tool", ToolName: "write", Arguments: args, HasResult: true, Text: "Wrote file"}}
	tab.expanded = true
	tab.folds["write-1"] = true
	tab.follow = false
	tab.offset = 0
	m.document(tab)
	return m
}
func inspectorTarget(t *testing.T, m *Model) hitRect {
	t.Helper()
	for y := 0; y < m.transcriptHeight(m.current()); y++ {
		for x := 0; x < m.mainWidth(); x++ {
			if _, rect, ok := m.toolInspectTarget(x, y); ok {
				return rect
			}
		}
	}
	t.Fatalf("no target in\n%s", ansi.Strip(m.current().doc.Text))
	return hitRect{}
}
func TestToolInspectorHoverOpenCloseAndNormalFold(t *testing.T) {
	m := inspectorFixture(t)
	tab := m.current()
	rect := inspectorTarget(t, m)
	if strings.Contains(m.View().Content, toolInspectButton) {
		t.Fatal("badge without hover")
	}
	before := strings.Split(ansi.Strip(m.View().Content), "\n")
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y})
	view := strings.Split(ansi.Strip(m.View().Content), "\n")
	if view[rect.y-1] != before[rect.y-1] {
		t.Fatal("hover changed top border")
	}
	if got := ansi.Cut(view[rect.y], rect.x, rect.x+rect.width); got != toolInspectButton {
		t.Fatalf("badge geometry: %q", got)
	}
	m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	if got := strings.Split(ansi.Strip(m.View().Content), "\n")[rect.y]; got != before[rect.y] {
		t.Fatal("content was not restored after hover")
	}
	m.Update(tea.MouseMotionMsg{X: rect.x, Y: rect.y})
	// The padded button segment is clickable too: no tool fold.
	click := tea.Mouse{Button: tea.MouseLeft, X: rect.x + rect.width - 1, Y: rect.y}
	m.mouse(click)
	m.mouseRelease(click)
	if !m.inspectorOpen() || !tab.folds["write-1"] || tab.selection.dragging {
		t.Fatal("icon clicked through to fold/selection")
	}
	if m.sidebarWidth() != 53 {
		t.Fatal("inspector did not replace sidebar with split")
	}
	// The complete marker may wrap across rows in the numbered change grid.
	// Remove only the visual gutter before joining continuations.
	var content strings.Builder
	for _, row := range m.inspectorRows() {
		plain := ansi.Strip(row)
		if at := strings.Index(plain, "│ "); at >= 0 {
			plain = plain[at+len("│ "):]
		}
		content.WriteString(plain)
	}
	all := content.String()
	if !strings.Contains(all, "line-01") || !strings.Contains(all, "END-35") {
		t.Fatal("full content missing")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "write /tmp/inspect.go") {
		t.Fatal("missing title")
	}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.inspectorOpen() || m.sidebarWidth() != 32 {
		t.Fatal("Esc did not restore sidebar")
	}
	rect = inspectorTarget(t, m)
	click = tea.Mouse{Button: tea.MouseLeft, X: rect.x - 3, Y: rect.y + 1}
	m.mouse(click)
	m.mouseRelease(click)
	if tab.folds["write-1"] {
		t.Fatal("ordinary click no longer folds")
	}
	if _, _, ok := m.toolInspectTarget(rect.x, rect.y); ok {
		t.Fatal("collapsed tool exposed inspector")
	}
}
func TestToolInspectorButtonPaddingClickable(t *testing.T) {
	for _, right := range []bool{false, true} {
		m := inspectorFixture(t)
		rect := inspectorTarget(t, m)
		x := rect.x
		if right {
			x += rect.width - 1
		}
		click := tea.Mouse{Button: tea.MouseLeft, X: x, Y: rect.y}
		m.mouse(click)
		m.mouseRelease(click)
		if !m.inspectorOpen() || !m.current().folds["write-1"] {
			t.Fatalf("padding click failed, right=%v", right)
		}
	}
}
func TestInspectorButtonsRevealOnlyNearby(t *testing.T) {
	m := inspectorFixture(t)
	r := inspectorTarget(t, m)
	for _, p := range [][2]int{{r.x - 3, r.y}, {r.x, r.y + 2}} {
		m.Update(tea.MouseMotionMsg{X: p[0], Y: p[1]})
		if strings.Contains(ansi.Strip(m.View().Content), toolInspectButton) {
			t.Fatal("view button revealed away from corner")
		}
	}
	m.Update(tea.MouseMotionMsg{X: r.x - 1, Y: r.y + 1})
	if !strings.Contains(ansi.Strip(m.View().Content), toolInspectButton) {
		t.Fatal("view button missing near corner")
	}
	for _, right := range []bool{false, true} {
		m.openInspector("write-1")
		left, width := m.inspectorBounds()
		m.Update(tea.MouseMotionMsg{X: left, Y: 3})
		before := strings.Split(ansi.Strip(m.inspectorView()), "\n")
		if !strings.Contains(before[1], inspectorCloseButton) {
			t.Fatal("close button must remain visible away from corner")
		}
		if !strings.HasPrefix(before[0], "┌") || !strings.HasSuffix(before[0], "┐") || !strings.HasPrefix(before[len(before)-1], "└") || !strings.HasSuffix(before[len(before)-1], "┘") {
			t.Fatal("missing sidebar frame")
		}
		x := left + width - m.inspectorInset() - ansi.StringWidth(inspectorCloseButton)
		if right {
			x = left + width - m.inspectorInset() - 1
		}
		m.Update(tea.MouseMotionMsg{X: x, Y: 1})
		if m.pointerShapeAt(x, 1) != "pointer" {
			t.Fatal("close padding missing pointer")
		}
		m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: 1})
		if m.inspectorOpen() || m.sidebarWidth() != 32 {
			t.Fatal("close padding did not restore sidebar")
		}
	}
}

func TestInspectorButtonColorsAndCloseAlignment(t *testing.T) {
	m := inspectorFixture(t)
	r := inspectorTarget(t, m)
	m.Update(tea.MouseMotionMsg{X: r.x - 1, Y: r.y})
	if !strings.Contains(m.View().Content, m.paint(toolInspectButton, m.cfg.Theme.Muted)) {
		t.Fatal("nearby view button should use muted color")
	}
	m.Update(tea.MouseMotionMsg{X: r.x, Y: r.y})
	if !strings.Contains(m.View().Content, m.paint(toolInspectButton, m.cfg.Theme.Text)) {
		t.Fatal("hovered view button should use text color")
	}
	m.openInspector("write-1")
	left, width := m.inspectorBounds()
	closeRect, ok := m.inspectorCloseRect()
	if !ok || closeRect.x+closeRect.width != left+width-m.inspectorInset() {
		t.Fatal("close button is not aligned to the content edge")
	}
	m.Update(tea.MouseMotionMsg{X: left, Y: 3})
	if !strings.Contains(m.inspectorView(), m.paint(inspectorCloseButton, m.cfg.Theme.Muted)) {
		t.Fatal("close button should use muted color by default")
	}
	line := strings.Split(ansi.Strip(m.inspectorView()), "\n")[1]
	if got := ansi.Cut(line, closeRect.x-left, width); got != inspectorCloseButton+" │" {
		t.Fatalf("unexpected close alignment: %q", got)
	}
	m.Update(tea.MouseMotionMsg{X: closeRect.x, Y: closeRect.y})
	if !strings.Contains(m.inspectorView(), m.paint(inspectorCloseButton, m.cfg.Theme.Text)) {
		t.Fatal("hovered close button should use text color")
	}
}

func TestToolInspectorFramedViewport(t *testing.T) {
	m := inspectorFixture(t)
	m.openInspector("write-1")
	m.sidebarHistory.pointerKnown = false
	left, width := m.inspectorBounds()
	view := strings.Split(ansi.Strip(m.inspectorView()), "\n")
	if len(view) != m.height || !strings.Contains(view[1], inspectorCloseButton) {
		t.Fatal("frame height or permanent close button incorrect")
	}
	for _, row := range view {
		if ansi.StringWidth(row) != width {
			t.Fatal("frame width incorrect")
		}
	}
	// Border and padding must never start a selection.
	for _, x := range []int{left, left + 1, left + width - 1} {
		m.inspectorClick(tea.Mouse{Button: tea.MouseLeft, X: x, Y: 2})
		if m.inspector.selection.dragging {
			t.Fatal("border selected")
		}
	}
	rows := m.inspectorRows()
	m.inspector.offset = len(rows)
	m.inspectorRows()
	if m.inspector.offset != len(rows)-m.inspectorBodyHeight() {
		t.Fatal("scroll clamp did not account for title and frame")
	}
	view = strings.Split(ansi.Strip(m.inspectorView()), "\n")
	if !strings.Contains(view[m.height-3], ansi.Strip(rows[len(rows)-1])) {
		t.Fatal("last result row hidden behind footer")
	}
}

func TestToolInspectorMaximumWidth(t *testing.T) {
	for _, width := range []int{160, 240} {
		m := inspectorFixture(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		m.openInspector("write-1")
		if got := m.sidebarWidth(); got != min(60, width/3) {
			t.Fatalf("terminal width %d: inspector width %d", width, got)
		}
		m.closeInspector()
		if got := m.sidebarWidth(); got != 32 {
			t.Fatalf("terminal width %d: restored sidebar width %d", width, got)
		}
	}
}
func TestToolInspectorScrollStreamingLifecycleAndCopy(t *testing.T) {
	m := inspectorFixture(t)
	tab := m.current()
	m.openInspector("write-1")
	rows := m.inspectorRows()
	if len(rows) < 100 {
		t.Fatal("long content did not wrap")
	}
	before := tab.offset
	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: m.mainWidth() + 5, Y: 3})
	if m.inspector.offset != 3 || tab.offset != before {
		t.Fatal("wheel scrolled main transcript")
	}
	// Body selection copies the inspector, not the transcript beneath it.
	m.inspector.offset = 0
	// Skip the status glyph and its separating space before selecting the label.
	x := m.mainWidth() + m.inspectorInset() + ansi.StringWidth("✓ ")
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: x, Y: 2})
	m.mouseMotion(tea.Mouse{Button: tea.MouseLeft, X: x + 6, Y: 2})
	if text := clipboardText(t, m.mouseRelease(tea.Mouse{Button: tea.MouseLeft, X: x + 6, Y: 2})); text != "written" {
		t.Fatalf("copied %q", text)
	}
	tab.state.Items[0].Arguments = json.RawMessage(`{"path":"/tmp/stream.go","content":"NEW_STREAM_CONTENT"}`)
	tab.state.Items[0].HasResult = false
	tab.state.Items[0].Running = true
	all := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	if !strings.Contains(all, "writing /tmp/stream.go") || !strings.Contains(all, "NEW_STREAM_CONTENT") || strings.Contains(all, "line-01") {
		t.Fatal("stale inspector cache")
	}
	tab.state.Items[0].HasResult = true
	tab.state.Items[0].Running = false
	if !strings.Contains(ansi.Strip(strings.Join(m.inspectorRows(), "\n")), "written /tmp/stream.go") {
		t.Fatal("completion state stale")
	}
	// Session replacement cannot leak the inspector into the next document.
	tab.state.Items = nil
	m.Update(tea.MouseMotionMsg{X: 0, Y: 0})
	if m.inspector.itemID != "" {
		t.Fatal("missing tool did not clear inspector")
	}
}
func TestToolInspectorGeometryAndModalPriority(t *testing.T) {
	for _, width := range []int{1, 2, 5, 30, 59, 60, 80, 160, 240} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := inspectorFixture(t)
			m.openInspector("write-1")
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			view := strings.Split(m.View().Content, "\n")
			if len(view) != 24 {
				t.Fatalf("height %d", len(view))
			}
			for _, line := range view {
				if ansi.StringWidth(line) > width {
					t.Fatalf("over-wide row %d > %d", ansi.StringWidth(line), width)
				}
			}
			if width < 60 && m.inputCursor(m.current()) != nil {
				t.Fatal("hidden editor cursor")
			}
			// Tiny terminals can always close through Esc.
			m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.inspectorOpen() {
				t.Fatal("cannot close")
			}
		})
	}
	m := inspectorFixture(t)
	m.openInspector("write-1")
	tab := m.current()
	tab.dialogs = []*dialog{{kind: "help", title: "Help"}}
	m.key(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.inspectorOpen() {
		t.Fatal("inspector consumed dialog Esc")
	}
	if len(tab.dialogs) != 0 {
		t.Fatal("dialog did not receive Esc")
	}
	left, w := m.inspectorBounds()
	if m.pointerShapeAt(left+w-3, 1) != "pointer" {
		t.Fatal("close pointer")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: left + w - 3, Y: 1})
	if m.inspectorOpen() {
		t.Fatal("close click failed")
	}
}
func TestToolInspectorDisablesSidebarMode(t *testing.T) {
	m := inspectorFixture(t)
	m.current().codex.State = codexfast.State{Level: "normal", Levels: []string{"normal", "fast", "ultrafast"}}
	target, _, ok := m.codexFastGeometry()
	if !ok {
		t.Fatal("missing sidebar mode target")
	}
	m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
	if !m.codexFastPopupVisible() {
		t.Fatal("mode popup did not open before inspector")
	}
	m.openInspector("write-1")
	for _, width := range []int{160, 60, 30} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		for y := 0; y < m.height; y++ {
			m.Update(tea.MouseMotionMsg{X: max(0, m.width-5), Y: y})
			if _, _, ok := m.codexFastGeometry(); ok {
				t.Fatal("hidden sidebar retained mode geometry")
			}
			if m.codexFastPopupVisible() || m.clickCodexFast(m.width-5, y) {
				t.Fatal("mode popup active under inspector")
			}
		}
	}
	m.closeInspector()
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	target, _, ok = m.codexFastGeometry()
	if !ok {
		t.Fatal("sidebar mode not restored")
	}
	m.Update(tea.MouseMotionMsg{X: target.x, Y: target.y})
	if !m.codexFastPopupVisible() {
		t.Fatal("restored sidebar mode cannot open")
	}
}

func TestToolInspectorToolKinds(t *testing.T) {
	for _, name := range []string{"read", "bash", "edit", "write", "codemode", "custom_tool"} {
		t.Run(name, func(t *testing.T) {
			m := inspectorFixture(t)
			tab := m.current()
			tab.state.Items[0].ToolName = name
			tab.state.Items[0].Text = "full tool output"
			m.document(tab)
			r := inspectorTarget(t, m)
			m.mouse(tea.Mouse{Button: tea.MouseLeft, X: r.x, Y: r.y})
			if !m.inspectorOpen() {
				t.Fatal("missing inspector")
			}
		})
	}
}

func TestToolInspectorDeltaUpdates(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	tab := m.current()
	apply := func(kind string, fields map[string]any) {
		fields["type"], fields["contentIndex"] = kind, 0
		raw, _ := json.Marshal(map[string]any{"type": "message_update", "assistantMessageEvent": fields})
		tab.state.Apply(raw)
		m.document(tab)
	}
	apply("toolcall_start", map[string]any{"id": "stream", "toolName": "write"})
	apply("toolcall_delta", map[string]any{"delta": `{"path":"stream.txt","content":"FIRST`})
	m.openInspector("tool:stream")
	first := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	apply("toolcall_delta", map[string]any{"delta": `\nSECOND`})
	second := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	if !strings.Contains(first, "FIRST") || strings.Contains(first, "SECOND") || !strings.Contains(second, "SECOND") || !strings.Contains(second, "writing stream.txt") {
		t.Fatal("partial JSON deltas not visible in inspector")
	}
	apply("toolcall_end", map[string]any{"toolCall": map[string]any{"type": "toolCall", "id": "stream", "name": "write", "arguments": map[string]string{"path": "final.txt", "content": "FINAL"}}})
	final := ansi.Strip(strings.Join(m.inspectorRows(), "\n"))
	if !strings.Contains(final, "FINAL") || strings.Contains(final, "FIRST") || !strings.Contains(final, "writing final.txt") {
		t.Fatal("final args did not replace preview")
	}
}

func TestToolInspectorTargetVisibleFrameOnly(t *testing.T) {
	m := inspectorFixture(t)
	rect := inspectorTarget(t, m)
	if _, _, ok := m.toolInspectTarget(m.mainWidth()-1, rect.y); ok {
		t.Fatal("outside frame targeted")
	}
	if _, _, ok := m.toolInspectTarget(rect.x, rect.y-2); ok {
		t.Fatal("tool header targeted as frame")
	}
	tab := m.current()
	tab.offset = rect.y + 1
	if _, _, ok := m.toolInspectTarget(rect.x, 1); ok {
		t.Fatal("offscreen frame header targeted")
	}
	tab.offset = 0
	tab.dialogs = []*dialog{{kind: "help"}}
	if _, _, ok := m.toolInspectTarget(rect.x, rect.y); ok {
		t.Fatal("modal backdrop target")
	}
	tab.dialogs = nil
	m.openInspector("write-1")
	m.activate(m.active)
	if m.inspectorOpen() {
		t.Fatal("activation retained old inspector")
	}
}
