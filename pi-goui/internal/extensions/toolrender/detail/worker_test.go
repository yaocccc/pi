package detail_test

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestWorkerReadableSectionsPreserveUnknownData(t *testing.T) {
	raw := `{"snapshotAt":12000,"futureBatch":"BATCH","tasks":[{"title":"检查代码","objective":"first\n\nlast","status":"running","mode":"scout","modelId":"provider/model","startedAt":1000,"usage":{"input":168234,"output":4321,"turns":2,"cacheRead":3,"cacheWrite":4,"contextTokens":1200,"futureUsage":"USAGE"},"activities":[{"label":"read","status":"completed","type":"tool","detail":"file.go","id":"ACTIVITY-ID","futureActivity":"ACTIVITY"}],"result":{"status":"blocked","summary":["SUMMARY"],"validation":[{"command":"go test","result":"passed"}],"futureResult":"RESULT"},"futureTask":"TASK"}]}`
	item := session.Item{ToolName: "worker", Details: json.RawMessage(raw)}
	text := renderDetailText(t, item, 60)
	for _, want := range []string{"! Task 1 · blocked", "Title\n检查代码", "Objective\nfirst\n\nlast", "Duration · 11s", "mode: scout", "modelId: provider/model", "input: 168234", "cacheRead: 3", "cacheWrite: 4", "contextTokens: 1200", "Activity · 1", "✓ read", "Result\nSummary", "Validation", "USAGE", "ACTIVITY", "RESULT", "TASK", "BATCH"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if string(item.Details) != raw || renderDetailText(t, item, 60) != text {
		t.Fatal("rendering mutated source")
	}
}

func TestWorkerActivityHidesOnlyProtocolMetadata(t *testing.T) {
	raw := `{"tasks":[{"status":"running","activities":[{"id":"HIDDEN_ID","at":1700000000123,"index":777,"contextSizeEstimate":998877,"type":"tool","status":"completed","label":"read","detail":"id: source-id\nat: source-time\n\n完整正文END","result":{"id":"RESULT_ID","at":"RESULT_AT"},"future":{"message":"KEEP_UNKNOWN"}}]}]}`
	text := renderDetailText(t, session.Item{ToolName: "worker", Details: json.RawMessage(raw)}, 60)
	for _, hidden := range []string{"HIDDEN_ID", "1700000000123", "777", "998877", "contextSizeEstimate", "type: tool", "status: completed"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("protocol field still visible %q: %s", hidden, text)
		}
	}
	for _, want := range []string{"✓ read", "id: source-id", "at: source-time", "完整正文END", "RESULT_ID", "RESULT_AT", "KEEP_UNKNOWN"} {
		if !strings.Contains(text, want) {
			t.Fatalf("activity content missing %q: %s", want, text)
		}
	}
}

func TestWorkerTranscriptVocabularyAndFullWidth(t *testing.T) {
	raw := `{"tasks":[{"title":"中文任务👩‍💻","objective":"完整目标","status":"completed","activities":[{"type":"thinking","label":"思考","detail":"很长的思考内容👩‍💻保持完整END_THOUGHT"},{"type":"tool","label":"read","status":"completed","detail":"源码第一行\n\n最后一行END_ACTIVITY"}],"result":{"status":"canceled","validation":[{"command":"go test 中文测试","result":"failed","details":"检查失败原因END_CHECK","future":"CHECK_EXTRA"}],"acceptance":[{"criterion":"完整结果","result":"passed","evidence":"证据"}]}},{"status":"queued"}]}`
	checkDetailWidths(t, session.Item{ToolName: "worker", Details: json.RawMessage(raw)}, "■ Task 1 · canceled", "├ ! 思考", "└ ✓ read", "END_THOUGHT", "END_ACTIVITY", "✗ failed", "✓ passed", "END_CHECK", "CHECK_EXTRA", "○ Task 2 · queued")
}

func TestWorkerIrregularFieldsAndPendingStatesRemainVisible(t *testing.T) {
	for _, status := range []string{"queued", "running", "completed", "cancelled", "unknown"} {
		data := map[string]any{"tasks": []any{map[string]any{"status": status, "modelId": map[string]any{"future": "MODEL"}, "usage": map[string]any{"input": map[string]any{"future": "INPUT"}}, "activities": []any{map[string]any{"label": []any{"LABEL"}, "detail": map[string]any{"future": "DETAIL"}}, "NON_OBJECT"}, "result": map[string]any{}}, "TASK_SCALAR"}}
		raw, _ := json.Marshal(data)
		text := renderDetailText(t, session.Item{ToolName: "worker", Details: raw}, 20)
		for _, want := range []string{"Task 1 · " + status, "MODEL", "INPUT", "LABEL", "DETAIL", "NON_OBJECT", "TASK_SCALAR", "{}"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q: %s", want, text)
			}
		}
	}
}
