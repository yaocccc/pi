package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

func TestInputFooterLayoutAndEffectiveKeys(t *testing.T) {
	for _, width := range []int{132, 100, 80, 60, 30, 12, 1} {
		m := testModel(t, `{"tui.input.submit":"alt+s","tui.input.newLine":"alt+j","app.interrupt":"alt+q"}`)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 36})
		tab := m.current()
		tab.state.Model = session.ModelInfo{ID: "gpt-6-astra"}
		tab.state.Thinking = "high"
		m.gitCwd = m.sidebarContext().Directory
		m.gitInfo = &render.GitStatus{Branch: "main", Modified: 2, Staged: 1}
		for _, value := range []string{"", "first\nsecond\nthird", strings.Repeat("word ", 80)} {
			tab.setValue(value)
			m.resizeEditor(tab)
			footer := m.inputFooter(tab)
			if width >= 100 && !strings.Contains(ansi.Strip(footer.text), " main · gpt-6-astra") {
				t.Fatalf("branch missing before model: %s", footer.text)
			}
			view := strings.Split(m.View().Content, "\n")
			if len(view) != m.height {
				t.Fatalf("height %d", len(view))
			}
			for _, line := range view {
				if ansi.StringWidth(line) > width {
					t.Fatalf("width=%d overflow %q", width, line)
				}
			}
			if width < 30 {
				continue
			}
			for _, rect := range []hitRect{footer.model, footer.thinking} {
				if rect.width == 0 {
					t.Fatal("lost visible model/think field")
				}
				got := ansi.Cut(ansi.Strip(view[rect.y]), rect.x, rect.x+rect.width)
				if !strings.Contains(ansi.Strip(footer.text), got) || strings.TrimSpace(got) == "" {
					t.Fatalf("wrong footer hit region %#v: %q", rect, got)
				}
				if rect.y != m.height-2 {
					t.Fatal("footer not inside frame")
				}
			}
			if width >= 100 {
				for _, hint := range []string{"alt+s", "alt+j"} {
					if !strings.Contains(ansi.Strip(footer.text), hint) {
						t.Fatalf("missing effective hint %s: %s", hint, footer.text)
					}
				}
			}
		}
	}
}
func TestInputFooterLongModelAndMouseBoundaries(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	tab := m.current()
	tab.state.Model = session.ModelInfo{ID: strings.Repeat("very-long-model-", 10)}
	tab.state.Thinking = "xhigh"
	footer := m.inputFooter(tab)
	if !strings.Contains(ansi.Strip(footer.text), "…") || !strings.Contains(ansi.Strip(footer.text), "xhigh") {
		t.Fatal("long model hides thinking")
	}
	if footer.model.x+footer.model.width >= footer.thinking.x {
		t.Fatal("model/think hitboxes overlap")
	}
	// One editable row plus a dedicated spacer above the footer.
	if tab.editor.Area.Height() != 1 || m.editorContentHeight(tab) != 1 || m.editorFrameHeight(tab) != 5 {
		t.Fatal("padding changed input height")
	}
	for _, p := range []tea.Mouse{{Button: tea.MouseLeft, X: footer.model.x, Y: footer.model.y - 1}, {Button: tea.MouseLeft, X: footer.model.x, Y: m.height - 1}} {
		if cmd := m.mouse(p); cmd != nil {
			t.Fatal("textarea/border click triggered footer command")
		}
	}
	tab.dialogs = append(tab.dialogs, m.newDialog("input", "Dialog", ""))
	if m.inputFooter(tab).visible {
		t.Fatal("footer active behind modal")
	}
}
func TestWorkingRowDirectlyAboveInputFrame(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	start := time.Now().Add(-2 * time.Second)
	tab.working.apply([]byte(`{"type":"agent_start"}`), start)
	tab.state.Busy = true
	tab.status = ""
	lines := strings.Split(m.View().Content, "\n")
	row := m.editorTop(tab) - 1
	if !strings.Contains(ansi.Strip(lines[row]), "1 turns") {
		t.Fatalf("working message not directly above frame: %q", lines[row])
	}
	if !strings.Contains(lines[row], "\x1b[") {
		t.Fatal("working message lost foreground styling")
	}
	if !strings.Contains(lines[row+1], "┌") {
		t.Fatal("spacer inserted below working message")
	}
	tab.state.Items = []session.Item{{Role: "assistant", Text: strings.Repeat("scroll\n\n", 100)}}
	m.document(tab)
	tab.follow = false
	tab.offset = 0
	label, rect, ok := m.latestButton(tab)
	if !ok || rect.y != m.editorTop(tab)-1 {
		t.Fatal("latest button is no longer on working row")
	}
	view := strings.Split(m.View().Content, "\n")
	if ansi.Cut(ansi.Strip(view[rect.y]), rect.x, rect.x+rect.width) != label {
		t.Fatal("latest hitbox mismatches painted button")
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: rect.x, Y: rect.y})
	if !tab.follow {
		t.Fatal("latest click didn't restore follow")
	}
}
func TestInputFooterDisabledKeysAndBlankSessionName(t *testing.T) {
	m := testModel(t, `{"tui.input.submit":[],"tui.input.newLine":[],"app.exit":[]}`)
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	footer := ansi.Strip(m.inputFooter(m.current()).text)
	if strings.Contains(footer, "^j") || strings.Contains(footer, "^d") || strings.Contains(footer, "↵") {
		t.Fatal("disabled defaults leaked into hints")
	}
	if !strings.Contains(footer, "— send") {
		t.Fatal("disabled action not represented")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "Session 1") || strings.Contains(ansi.Strip(m.View().Content), "Untitled") {
		t.Fatal("blank session name replaced with fallback")
	}
}
