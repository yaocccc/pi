package input

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestLegacyResizeReproducesInitialNewlineScroll(t *testing.T) {
	e := New()
	e.Area.Focus()
	e.Area.SetWidth(30)
	e.Area.SetHeight(1)
	e.SetValue("first line")
	e.Update("tui.input.newLine", tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	e.Area.SetWidth(30)
	e.Area.SetHeight(2)
	if e.Area.ScrollYOffset() != 1 {
		t.Fatalf("expected legacy stale offset 1, got %d", e.Area.ScrollYOffset())
	}
	t.Log("legacy resize leaves viewport offset 1 after height grows to 2")
}
func TestResizeUsesNativeWordWrap(t *testing.T) {
	e := New()
	e.Area.Focus()
	e.SetValue("123456789012345 123456789012345 123456789012345")
	e.Resize(30, 7)
	if e.Area.Height() != 3 || e.Area.ScrollYOffset() != 0 {
		t.Fatalf("native word-wrap height=%d offset=%d", e.Area.Height(), e.Area.ScrollYOffset())
	}
	text := strings.Repeat("界e\u0301👩‍💻 ", 8)
	e.SetValue(text)
	row, col := e.Area.Line(), e.Area.Column()
	e.Resize(20, 7)
	if e.Value() != text || e.Area.Line() != row || e.Area.Column() != col || e.Area.Height() <= 1 {
		t.Fatal("Unicode reflow changed content/cursor or failed to wrap")
	}
	for _, line := range strings.Split(e.Area.View(), "\n") {
		if ansi.StringWidth(line) > 20 {
			t.Fatalf("wide Unicode row: %q", line)
		}
	}
}

func TestResizePreservesUnlimitedContentAndCursor(t *testing.T) {
	e := New()
	e.Area.Focus()
	e.Resize(40, 4)
	content := strings.Repeat("a line\n", 160) + "tail"
	e.Update("", tea.PasteMsg{Content: content})
	e.Resize(40, 4)
	if e.Value() != content || e.Area.LineCount() != 161 {
		t.Fatal("input was truncated")
	}
	row, col := e.Area.Line(), e.Area.Column()
	e.Resize(25, 3)
	if e.Area.Line() != row || e.Area.Column() != col || e.Area.Height() != 3 {
		t.Fatal("resize moved cursor or violated height")
	}
	if e.Area.ScrollYOffset() == 0 {
		t.Fatal("long content should scroll inside its bounded view")
	}
	e.Update("tui.editor.undo", tea.KeyPressMsg{})
	e.Resize(25, 3)
	if e.Value() != "" || e.Area.ScrollYOffset() != 0 || e.Area.Height() != 1 {
		t.Fatal("undo did not restore empty, unscrolled editor")
	}
}
