package input

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/rivo/uniseg"
)

func TestNativeWordPunctuation(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		{"foo-bar", "foo-"}, {"foo/bar", "foo/"}, {"foo_bar", ""},
		{"\"some/path\"", "\"some/path"}, {"foo...!?", "foo"},
		{"foo bar  ", "foo "}, {"foo-e\u0301", "foo-"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			e := editor()
			e.SetValue(tc.text)
			press(&e, "tui.editor.deleteWordBackward")
			if e.Value() != tc.want {
				t.Fatalf("got %q want %q", e.Value(), tc.want)
			}
			press(&e, "tui.editor.yank")
			if e.Value() != tc.text {
				t.Fatalf("yank got %q", e.Value())
			}
			press(&e, "tui.editor.undo")
			if e.Value() != tc.want {
				t.Fatalf("undo got %q", e.Value())
			}
		})
	}
	e := editor()
	e.SetValue("foo-bar")
	press(&e, "tui.editor.cursorLineStart")
	for _, column := range []int{3, 4, 7} {
		press(&e, "tui.editor.cursorWordRight")
		if e.Area.Column() != column {
			t.Fatalf("right column %d want %d", e.Area.Column(), column)
		}
	}
	for _, column := range []int{4, 3, 0} {
		press(&e, "tui.editor.cursorWordLeft")
		if e.Area.Column() != column {
			t.Fatalf("left column %d want %d", e.Area.Column(), column)
		}
	}
	e.SetValue("foo/bar")
	press(&e, "tui.editor.cursorLineStart")
	press(&e, "tui.editor.deleteWordForward")
	if e.Value() != "/bar" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.deleteWordForward")
	if e.Value() != "bar" {
		t.Fatal(e.Value())
	}
}

func TestWordMultilineAndUnicodeSafety(t *testing.T) {
	if got := piWordBoundary("one\t two", 8, true); got != 5 {
		t.Fatalf("tab word boundary: %d", got)
	}
	e := editor()
	e.SetValue("first\nsecond")
	press(&e, "tui.editor.cursorWordLeft")
	if e.Area.Line() != 1 || e.Area.Column() != 0 {
		t.Fatal(e.snapshot())
	}
	press(&e, "tui.editor.cursorWordLeft")
	if e.Area.Line() != 0 || e.Area.Column() != 0 {
		t.Fatal(e.snapshot())
	}
	for _, text := range []string{"中文路径/你好", "hello-e\u0301", "foo-👨‍👩‍👧‍👦", "你好 世界", "é\u0301—世界"} {
		boundaries := map[int]bool{0: true}
		g := uniseg.NewGraphemes(text)
		n := 0
		for g.Next() {
			n += utf8.RuneCountInString(g.Str())
			boundaries[n] = true
		}
		for pos := 0; pos < n; {
			next := piWordBoundary(text, pos, false)
			if next <= pos || !boundaries[next] {
				t.Fatalf("%q forward %d -> %d splits grapheme/no progress", text, pos, next)
			}
			pos = next
		}
		for pos := n; pos > 0; {
			next := piWordBoundary(text, pos, true)
			if next >= pos || !boundaries[next] {
				t.Fatalf("%q backward %d -> %d splits grapheme/no progress", text, pos, next)
			}
			pos = next
		}
	}
}

func TestUnlimitedMultilinePasteUndoYank(t *testing.T) {
	e := editor()
	content := strings.Repeat("line 你好\n", 160) + "last"
	e.Update("", tea.PasteMsg{Content: content})
	if e.Value() != content || e.Area.LineCount() != 161 {
		t.Fatalf("paste truncated: %d lines", e.Area.LineCount())
	}
	if e.Area.Height() != 4 {
		t.Fatalf("viewport changed: %d", e.Area.Height())
	}
	press(&e, "tui.editor.deleteWordBackward")
	if e.Value() != strings.TrimSuffix(content, "last") {
		t.Fatal("delete changed earlier lines")
	}
	press(&e, "tui.editor.undo")
	if e.Value() != content {
		t.Fatal("undo restore truncated")
	}
	press(&e, "tui.editor.undo")
	if e.Value() != "" {
		t.Fatal("paste undo failed")
	}
	// A multiline kill may come from sequential kills or a prior draft; yank and
	// yank-pop restoration must not reintroduce Bubbles' legacy 99-line limit.
	e.state.kills = []string{content, "short"}
	press(&e, "tui.editor.yank")
	if e.Value() != content {
		t.Fatal("yank truncated")
	}
	press(&e, "tui.editor.yankPop")
	if e.Value() != "short" {
		t.Fatal("yank-pop failed")
	}
	press(&e, "tui.editor.undo")
	if e.Value() != content {
		t.Fatal("yank-pop undo truncated")
	}
	e.SetValue(content)
	if e.Value() != content {
		t.Fatal("external value truncated")
	}
}
