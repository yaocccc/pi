package input

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

func editor() Editor            { e := New(); e.Area.SetWidth(60); e.Area.SetHeight(4); e.Area.Focus(); return e }
func press(e *Editor, a string) { e.Update(a, tea.KeyPressMsg{}) }
func TestTypingUndoPaste(t *testing.T) {
	e := editor()
	for _, s := range []string{"你", "好", "🌍"} {
		e.Update("", tea.KeyPressMsg{Text: s})
	}
	if e.Value() != "你好🌍" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.undo")
	if e.Value() != "" {
		t.Fatal(e.Value())
	}
	e.Update("app.clear", tea.PasteMsg{Content: "ctrl+c\n你好"})
	if e.Value() != "ctrl+c\n你好" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.undo")
	if e.Value() != "" {
		t.Fatal(e.Value())
	}
}
func TestDisabledShortcuts(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'h', Mod: tea.ModCtrl}, {Code: 'j', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModCtrl}, {Code: 'w', Mod: tea.ModCtrl}, {Code: 'd', Mod: tea.ModAlt}, {Code: tea.KeyEnter}, {Code: tea.KeyBackspace}} {
		e := editor()
		e.SetValue("safe")
		e.Update("", key)
		if e.Value() != "safe" {
			t.Fatalf("%+v => %q", key, e.Value())
		}
	}
}
func TestSemanticBindings(t *testing.T) {
	e := editor()
	e.SetValue("one two")
	e.Update("tui.editor.deleteWordBackward", tea.KeyPressMsg{Code: 'q', Text: "q"})
	if e.Value() != "one " {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.yank")
	if e.Value() != "one two" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.input.newLine")
	e.Update("", tea.KeyPressMsg{Text: "three"})
	if e.Value() != "one two\nthree" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.cursorLineStart")
	if e.Area.Column() != 0 || e.Area.Line() != 1 {
		t.Fatal(e.Area.Column(), e.Area.Line())
	}
}
func TestUnicodeAndUndoPosition(t *testing.T) {
	e := editor()
	e.SetValue("你é👨‍👩‍👧‍👦")
	press(&e, "tui.editor.deleteCharBackward")
	if e.Value() != "你é" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.cursorLeft")
	if e.Area.Column() != 1 {
		t.Fatal(e.Area.Column())
	}
	press(&e, "tui.editor.deleteCharForward")
	if e.Value() != "你" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.undo")
	if e.Value() != "你é" || e.Area.Column() != 1 {
		t.Fatal(e.Value(), e.Area.Column())
	}
}
func TestKillYankPop(t *testing.T) {
	e := editor()
	e.SetValue("one")
	press(&e, "tui.editor.deleteToLineStart")
	e.Update("", tea.KeyPressMsg{Text: "two"})
	press(&e, "tui.editor.deleteToLineStart")
	press(&e, "tui.editor.yank")
	if e.Value() != "two" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.yankPop")
	if e.Value() != "one" {
		t.Fatal(e.Value())
	}
	press(&e, "tui.editor.yankPop")
	if e.Value() != "two" {
		t.Fatal(e.Value())
	}
}
func TestJumpAndExternalReplacement(t *testing.T) {
	e := editor()
	e.SetValue("a b a")
	press(&e, "tui.editor.cursorLineStart")
	press(&e, "tui.editor.jumpForward")
	if !e.Jumping() {
		t.Fatal("not waiting")
	}
	e.Update("", tea.KeyPressMsg{Code: 'a', Text: "a"})
	if e.Area.Column() != 4 || e.Jumping() {
		t.Fatal(e.Area.Column())
	}
	press(&e, "tui.editor.jumpBackward")
	e.Update("", tea.KeyPressMsg{Code: tea.KeyEscape})
	if e.Jumping() {
		t.Fatal("not cancelled")
	}
	e.Update("", tea.KeyPressMsg{Text: "x"})
	e.SetValue("external")
	press(&e, "tui.editor.undo")
	if e.Value() != "external" {
		t.Fatal(e.Value())
	}
}
func TestLogicalLineRestore(t *testing.T) {
	e := editor()
	e.Area.SetWidth(8)
	e.SetValue("long long long\n你好\nlast")
	press(&e, "tui.editor.deleteCharBackward")
	press(&e, "tui.editor.undo")
	if e.Area.Line() != 2 || e.Area.Column() != 4 || e.Value() != "long long long\n你好\nlast" {
		t.Fatal(e.Area.Line(), e.Area.Column(), e.Value())
	}
}

func TestCompletionPreservesUnicodeSuffixCursorAndUndo(t *testing.T) {
	e := New()
	e.Area.Focus()
	e.SetValue("前文 @文 后文")
	e.Resize(20, 3)
	e.CompleteRange(3, 5, "@\"文件 名.go\"", 11)
	if e.Value() != "前文 @\"文件 名.go\" 后文" {
		t.Fatal(e.Value())
	}
	if e.CursorOffset() != 13 {
		t.Fatalf("wrong rune cursor: %d", e.CursorOffset())
	}
	e.Update("tui.editor.undo", tea.KeyPressMsg{})
	if e.Value() != "前文 @文 后文" {
		t.Fatal("completion not undoable")
	}
}
