package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func TestCodemodeCallHierarchyAndUnknownFields(t *testing.T) {
	item := session.Item{ToolName: "codemode", Running: true, Arguments: json.RawMessage(`{"code":"await tools.read({path: '中文.go'})"}`), Details: json.RawMessage(`{"calls":[{"id":9,"name":"read","status":"running","durationMs":12.5,"args":"{\"path\":\"中文.go\",\"offset\":3}","result":{"text":"COMPLETE_RESULT"},"future":{"deep":["retained"]}},{"name":"bash","status":"error","error":"first\nsecond","args":"plain text"}],"other":"kept"}`), Text: "partial\n\n  output"}
	theme := config.Theme{Warning: "warning", Error: "error"}
	hooks := testHooks(t)
	colors := map[string]string{}
	hooks.Line = func(text, color string, width int) []string { colors[text] = color; return strings.Split(text, "\n") }
	text := strings.Join(Render(item, theme, 30, hooks), "\n")
	for _, want := range []string{"Running script", "Script", "Tool calls", "#1 · read · ⋯ running", "durationMs: 12.5 ms", "Arguments\noffset: 3\npath: 中文.go", "COMPLETE_RESULT", "deep", "retained", "id: 9", "#2 · bash · ✗ error", "Error\nfirst\nsecond", "plain text", "other: kept", "partial\n\n  output"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if colors["#1 · read · ⋯ running"] != theme.Warning || colors["#2 · bash · ✗ error"] != theme.Error || colors["first\nsecond"] != theme.Error {
		t.Fatal(colors)
	}
	if strings.Contains(text, "Script finished") {
		t.Fatal(text)
	}
}

func TestCodemodeEnvelopePreservesOutputAndExactCallMetadata(t *testing.T) {
	item := session.Item{ToolName: "codemode", HasResult: true,
		Arguments: json.RawMessage(`{"code":"console.log('x')","future":{"full":"kept"}}`),
		Details:   json.RawMessage(`{"calls":[{"name":"read","status":"ok","cost":0.000000000000123456789,"durationMs":123.456789,"args":"{\"id\":9007199254740993}"}]}`),
		Text:      "Script completed\nWall time 1.234 seconds\nOutput:\n\n  FIRST\n\nLAST  \n"}
	text := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	for _, want := range []string{"› Script", "Parameters", "full: kept", "✓ Script completed", "Wall time · 1.234 seconds", "Tool calls · 1", "cost: $0.000000000000123456789", "durationMs: 123.456789 ms", "id: 9007199254740993", "Output\n\n  FIRST\n\nLAST  \n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "Output:") || strings.Count(text, "Script completed") != 1 {
		t.Fatalf("executor envelope duplicated: %s", text)
	}
	// Ordinary script output resembling the envelope is never partially eaten.
	item.Text = "Script completed\nWall time unknown seconds\nOutput:\nkept"
	if got := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n"); !strings.Contains(got, item.Text) {
		t.Fatal(got)
	}
}

func TestCodemodePreservesUnexpectedCallValues(t *testing.T) {
	item := session.Item{ToolName: "codemode", HasResult: true, Details: json.RawMessage(`{"calls":[null,{"name":"future","status":{"new":"state"},"durationMs":{"unknown":true},"args":"001","result":"{not json}","error":{"code":99}}]}`)}
	text := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	for _, want := range []string{"Script finished", "—", "new: state", "durationMs", "unknown: yes", "Arguments\n001", "Result\n{not json}", "Error\ncode: 99"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
