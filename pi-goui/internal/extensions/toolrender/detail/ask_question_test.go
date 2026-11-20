package detail_test

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestQuestionOptionsAndRecordedAnswers(t *testing.T) {
	args := `{"questions":[{"label":"语言","question":"Choose languages","options":["Go","TS"],"multiSelect":true,"future":"INPUT_META"}]}`
	details := `{"questions":[{"label":"语言","question":"Choose languages","options":["Go","TS"],"multiSelect":true,"answer":["TS","custom"],"customAnswers":["custom"],"future":"ANSWER_META"}],"futureBatch":"BATCH_META"}`
	item := session.Item{ToolName: "ask_question", Arguments: json.RawMessage(args), Details: json.RawMessage(details)}
	text := renderDetailText(t, item, 50)
	for _, want := range []string{"Questions", "Question 1 · 语言", "Multiple choices", "○ Go", "Answers", "✓ TS", "answer", "customAnswers", "custom", "INPUT_META", "ANSWER_META", "BATCH_META"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, "Choose languages") != 1 || strings.Count(text, "✓ TS") != 1 {
		t.Fatal("question or answer card duplicated")
	}
	if strings.Contains(text, "✓ Go") {
		t.Fatal("inferred unselected answer")
	}
	if string(item.Arguments) != args || string(item.Details) != details {
		t.Fatal("mutated source")
	}
}

func TestQuestionCustomAnswerDoesNotSelectMatchingOption(t *testing.T) {
	item := session.Item{ToolName: "ask_question", HasResult: true, Arguments: json.RawMessage(`{"question":"Pick?","options":["A","B"]}`), Details: json.RawMessage(`{"answer":"A","wasCustom":true,"customAnswers":["A"]}`)}
	text := renderDetailText(t, item, 40)
	if !strings.Contains(text, "○ A") || !strings.Contains(text, "✓ Custom · A") || strings.Contains(text, "✓ A") {
		t.Fatal(text)
	}
	item.Details = json.RawMessage(`{"cancelled":true,"answer":"A"}`)
	text = renderDetailText(t, item, 40)
	if strings.Contains(text, "✓") || !strings.Contains(text, "answer: A") {
		t.Fatal("cancelled selection presented as confirmed or lost: " + text)
	}
}

func TestQuestionHangingOptionsAndFullContent(t *testing.T) {
	raw := `{"questions":[{"question":"中文问题👩‍💻","options":["完整选项第一行\n第二行END_OPTION"],"answer":"自定义答案END_ANSWER","wasCustom":true,"unknown":"保留END_META"}]}`
	checkDetailWidths(t, session.Item{ToolName: "ask_question", HasResult: true, Details: json.RawMessage(raw)}, "○ 完整选项第一行\n  第二行END_OPTION", "✓ Custom · 自定义答案END_ANSWER", "保留END_META")
}

func TestQuestionUnansweredCancelledAndLegacy(t *testing.T) {
	item := session.Item{ToolName: "ask_question", Arguments: json.RawMessage(`{"question":"Choose?","options":["A","B"],"multiSelect":false}`), Details: json.RawMessage(`{"cancelled":true,"questions":[{"question":"Choose?","options":["A","B"],"answer":null,"multiSelect":false,"unknown":{"nested":"KEPT"}}]}`)}
	text := renderDetailText(t, item, 30)
	for _, want := range []string{"Cancelled", "Single choice", "No answer recorded", "answer: —", "KEPT", "cancelled: yes"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
	if strings.Contains(text, "✓") || strings.Contains(text, "Awaiting") {
		t.Fatal("cancelled answer misrepresented")
	}
	item.Details = json.RawMessage(`{"question":"Choose?","options":["A","B"],"answer":"B","wasCustom":false,"future":"LEGACY_META"}`)
	text = renderDetailText(t, item, 30)
	if !strings.Contains(text, "✓ B") || !strings.Contains(text, "answer: B") || !strings.Contains(text, "LEGACY_META") {
		t.Fatal(text)
	}
}
