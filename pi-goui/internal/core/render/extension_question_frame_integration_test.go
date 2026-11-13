package render

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestExtensionQuestionFrameIntegration(t *testing.T) {
	item := visualItem("ask_question", map[string]any{"question": "显示哪些信息？", "options": []string{"简洁", "标准", "详细"}}, map[string]any{"question": "显示哪些信息？", "options": []string{"简洁", "标准", "详细"}, "answer": "简洁", "multiSelect": false, "wasCustom": false}, "用户选择：简洁")
	rows := toolBody(item, testTheme(), Options{Tools: testToolViews(), ExpandTools: true}, 120)
	if len(rows) < 2 || !strings.Contains(ansi.Strip(rows[1]), "╭─ ask_question") {
		t.Fatalf("missing extension frame: %v", rows)
	}
	if ansi.StringWidth(rows[1]) >= 60 {
		t.Fatal("question frame not compact")
	}
}
