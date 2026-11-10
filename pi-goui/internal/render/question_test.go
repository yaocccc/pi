package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestQuestionCardHighlightsAnswerWithoutDuplication(t *testing.T) {
	args := map[string]any{"question": "显示哪些信息？", "options": []string{"简洁", "标准", "详细"}}
	details := map[string]any{"question": "显示哪些信息？", "options": []string{"简洁", "标准", "详细"}, "answer": "简洁", "multiSelect": false, "wasCustom": false}
	item := visualItem("ask_question", args, details, "用户选择：简洁")
	rows := toolBody(item, testTheme(), Options{ExpandTools: true}, 120)
	plain := ansi.Strip(strings.Join(rows[1:], "\n"))
	for _, want := range []string{"╭─ ask_question", "◦ 显示哪些信息？", "✓ 简洁", "○ 标准", "○ 详细"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q: %s", want, plain)
		}
	}
	for _, bad := range []string{"Parameters", "Result", "Metadata", "Questions and choices", "用户选择：", "Multi select", "Was custom", "等待回答"} {
		if strings.Contains(plain, bad) {
			t.Fatal("redundant detail: " + bad)
		}
	}
	if strings.Count(plain, "显示哪些信息？") != 1 || strings.Count(plain, "简洁") != 1 {
		t.Fatal("question or answer duplicated")
	}
	if ansi.StringWidth(rows[1]) >= 60 {
		t.Fatal("question frame not compact")
	}
	raw := strings.Join(rows, "\n")
	if !strings.Contains(raw, paint("简洁", testTheme().Success)) {
		t.Fatal("selected answer not highlighted")
	}
}

func TestQuestionCardMultipleCustomAndCancelled(t *testing.T) {
	args := map[string]any{"questions": []any{
		map[string]any{"question": "选择项目", "multiSelect": true, "options": []string{"A", "B", "C"}},
		map[string]any{"question": "下一步？", "options": []string{"继续", "结束"}},
	}}
	details := map[string]any{"questions": []any{
		map[string]any{"answer": []string{"B", "自定义文本"}, "customAnswers": []string{"自定义文本"}},
		map[string]any{"answer": "结束"},
	}}
	item := visualItem("ask_question", args, details, "用户回答了 2 个问题")
	plain := ansi.Strip(visualText(item, 100))
	for _, want := range []string{"1. 选择项目（多选）", "○ A", "✓ B", "○ C", "✓ 自定义：自定义文本", "2. 下一步？", "✓ 结束"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q: %s", want, plain)
		}
	}
	if strings.Contains(plain, "用户回答了") {
		t.Fatal("raw result duplicated answers")
	}
	for _, width := range []int{1, 2, 4, 8, 20, 80} {
		checkWidth(t, visualText(item, width), width)
	}
	details["cancelled"] = true
	item = visualItem("ask_question", args, details, "用户取消了问卷。")
	plain = ansi.Strip(visualText(item, 100))
	if !strings.Contains(plain, "已取消") || strings.Contains(plain, "✓") {
		t.Fatal("cancelled questionnaire retained selections")
	}
}

func TestQuestionCardWaitingAndErrors(t *testing.T) {
	item := visualItem("ask_question", map[string]any{"question": "Pick", "options": []string{"A", "B"}}, nil, "")
	item.HasResult, item.Running = false, true
	if !strings.Contains(ansi.Strip(visualText(item, 80)), "等待回答") {
		t.Fatal("missing pending state")
	}
	item.Running, item.Error = false, true
	item.Text = "Request failed"
	plain := ansi.Strip(visualText(item, 80))
	if !strings.Contains(plain, "Request failed") || strings.Contains(plain, "等待回答") {
		t.Fatal("incorrect failed state")
	}
	item.Error, item.HasResult = false, true
	item.Text = "用户取消了选择。"
	if !strings.Contains(ansi.Strip(visualText(item, 80)), "已取消") {
		t.Fatal("missing legacy cancellation state")
	}
	item = visualItem("ask_question", map[string]any{"question": "unsafe\u001b[2J", "options": []string{"safe"}}, map[string]any{"answer": "\u001b]52;c;SECRET\u0007custom", "wasCustom": true}, "")
	if strings.Contains(visualText(item, 80), "\u001b]52") || strings.Contains(visualText(item, 80), "\u001b[2J") {
		t.Fatal("untrusted terminal escape leaked")
	}
}
