package input

import (
	tea "charm.land/bubbletea/v2"
	"testing"
)

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
