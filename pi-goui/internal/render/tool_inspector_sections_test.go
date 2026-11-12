package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func inspectorText(item session.Item, width int) string {
	return ansi.Strip(strings.Join(ToolResultRows(item, testTheme(), width), "\n"))
}

func TestInspectorSemanticSections(t *testing.T) {
	tests := []struct {
		name          string
		args, details map[string]any
		output        string
		want          []string
	}{
		{"read", map[string]any{"path": "source.go", "offset": 7}, map[string]any{"truncation": map[string]any{"content": "package main", "truncated": true}, "extra": "read metadata"}, "package main\n\n[Showing lines 7-7. Use offset=8 to continue.]", []string{"File · source.go · from line 7", "7 │ package main", "Use offset=8", "truncated: yes", "extra: read metadata"}},
		{"write", map[string]any{"path": "new.go", "content": "package main\n" + strings.Repeat("// middle\n", 25) + "// last"}, nil, "saved", []string{"written new.go", "1 │ package main", "27 │ // last", "Output", "saved"}},
		{"edit", map[string]any{"path": "change.go"}, map[string]any{"diff": "-1 old\n+1 new", "extra": map[string]any{"revision": "future"}}, "edited", []string{"Diff · +1 −1", "-1 old", "+1 new", "revision: future"}},
		{"bash", map[string]any{"command": "printf '%s' done"}, map[string]any{"exitCode": 0, "fullOutputPath": "/tmp/output.log"}, "done", []string{"Command", "printf", "Exit code", "0", "Output", "done", "fullOutputPath: /tmp/output.log"}},
		{"codemode", map[string]any{"code": "const n = 1;"}, map[string]any{"calls": []any{map[string]any{"name": "read", "status": "ok", "args": "{\"path\":\"file.go\"}", "result": map[string]any{"nested": "NESTED_RESULT"}, "durationMs": 42, "future": "UNKNOWN_FIELD"}}}, "Script completed\nWall time 1 seconds\nOutput:\nHELLO", []string{"Script", "const n = 1;", "Tool calls", "#1 · read", "Arguments", "file.go", "NESTED_RESULT", "durationMs: 42", "UNKNOWN_FIELD", "HELLO"}},
		{"ask_question", map[string]any{"questions": []any{map[string]any{"question": "Which color?", "options": []any{"red", "blue"}}}}, map[string]any{"questions": []any{map[string]any{"question": "Which color?", "answer": "blue", "future": "ANSWER_METADATA"}}}, "selected blue", []string{"Questions", "Which color?", "○ red", "✓ blue", "Answers", "answer: blue", "ANSWER_METADATA"}},
		{"custom", nil, map[string]any{"future": []any{"a", false, nil}}, `{"answer":{"value":42}}`, []string{"Result", "answer", "value: 42", "Details", "future", "• no", "• —"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := visualItem(tt.name, tt.args, tt.details, tt.output)
			text := inspectorText(item, 100)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q:\n%s", want, text)
				}
			}
			if tt.name == "read" && strings.Count(text, "package main") != 1 {
				t.Fatal("repeated read payload")
			}
			if tt.name == "edit" && strings.Count(text, "+1 new") != 1 {
				t.Fatal("repeated diff payload")
			}
		})
	}
}

func TestInspectorWorkerTaskSectionsAndUnknownFields(t *testing.T) {
	item := session.Item{ToolName: "worker", Details: json.RawMessage(`{"kind":"worker-ui","snapshotAt":12000,"futureBatch":"BATCH_EXTRA","tasks":[{"title":"Inspect source","objective":"Read the complete source and verify the output","mode":"scout","status":"completed","startedAt":1000,"finishedAt":11000,"usage":{"input":168234,"output":4321,"turns":2},"activities":[{"label":"read","detail":"all activity data","status":"completed","futureActivity":"ACTIVITY_EXTRA"}],"result":{"summary":["Worker finished"],"validation":[{"command":"go test","result":"passed","details":"ALL_CHECKS"}],"execution":{"actual_model_id":"model-a"},"futureResult":"RESULT_EXTRA"},"futureTask":"TASK_EXTRA"}]}`)}
	text := inspectorText(item, 100)
	for _, want := range []string{"Tasks · 1", "Task 1 · completed", "Title", "Inspect source", "Objective", "Read the complete source", "Duration · 10s", "Usage", "input: 168234", "Activity", "all activity data", "Result", "Worker finished", "ALL_CHECKS", "ACTIVITY_EXTRA", "RESULT_EXTRA", "TASK_EXTRA", "BATCH_EXTRA", "model-a"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"tasks"`) || strings.Contains(text, `"usage"`) {
		t.Fatal("raw snapshot instead of sections")
	}
	if strings.Count(text, "Worker finished") != 1 {
		t.Fatal("duplicated task result")
	}
	item.Text = string(item.Details)
	if inspectorText(item, 100) != text {
		t.Fatal("duplicated identical JSON text/details")
	}
	var snapshot map[string]any
	if err := json.Unmarshal(item.Details, &snapshot); err != nil {
		t.Fatal(err)
	}
	task := snapshot["tasks"].([]any)[0].(map[string]any)
	payload := map[string]any{"result": map[string]any{"results": []any{task["result"]}}, "future": "PAYLOAD_EXTRA"}
	snapshot["payload"] = payload
	item.Details, _ = json.Marshal(snapshot)
	data, _ := json.Marshal(payload)
	item.Text = string(data)
	text = inspectorText(item, 100)
	if strings.Count(text, "Worker finished") != 1 || !strings.Contains(text, "See Task 1 result above") || !strings.Contains(text, "PAYLOAD_EXTRA") {
		t.Fatalf("duplicate payload or metadata lost:\n%s", text)
	}
	if again := inspectorText(item, 100); again != text {
		t.Fatal("rendering mutated snapshot")
	}
}

func TestInspectorSectionsWidthSafetyAndFullUnknownValues(t *testing.T) {
	long := strings.Repeat("x", 300) + "END_MARKER"
	items := []session.Item{
		visualItem("read", map[string]any{"path": "a.go"}, nil, long),
		visualItem("edit", nil, map[string]any{"diff": "-1 old\n+1 " + long}, ""),
		visualItem("worker", nil, map[string]any{"tasks": []any{map[string]any{"objective": "中文👩‍💻", "result": map[string]any{"future": long}}}}, ""),
		visualItem("codemode", map[string]any{"code": long}, map[string]any{"calls": []any{map[string]any{"name": "test", "args": long}}}, ""),
	}
	for _, width := range []int{1, 2, 8, 20, 56, 100} {
		for _, item := range items {
			rows := ToolResultRows(item, testTheme(), width)
			var joined strings.Builder
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("%s overflow %d: %q", item.ToolName, width, row)
				}
				line := ansi.Strip(row)
				if at := strings.Index(line, "│ "); at >= 0 {
					line = line[at+len("│ "):]
				}
				joined.WriteString(line)
			}
			if !strings.Contains(joined.String(), "END_MARKER") {
				t.Fatalf("truncated %s at width %d", item.ToolName, width)
			}
		}
	}
}

// Exercise the actual ANSI, grapheme and wrapping hooks through the public
// entry point, rather than the lightweight hooks used by individual renderers.
func TestInspectorAdditionalToolsIntegration(t *testing.T) {
	payload := "中文👩‍💻\n\n  " + strings.Repeat("LONG_CONTENT_", 40) + "PAYLOAD_END\n"
	tests := []struct {
		name, heading string
		args, details map[string]any
	}{
		{"web_search", "Search · returned", map[string]any{"queries": []any{"中文👩‍💻"}}, map[string]any{"queries": []any{"中文👩‍💻"}, "totalResults": 3, "queryProviders": []any{map[string]any{"query": "中文", "providers": []any{"exa"}}}}},
		{"fetch_content", "Fetch · returned", map[string]any{"url": "https://example.test/中文"}, map[string]any{"urls": []any{"https://example.test/中文"}, "title": "中文👩‍💻", "successful": 1}},
		{"source_check", "Source check · returned", map[string]any{"claim": "中文👩‍💻"}, map[string]any{"artifact": map[string]any{"sources": []any{map[string]any{"title": "中文", "url": "https://example.test", "futureSource": "SOURCE_UNKNOWN"}}, "passages": []any{map[string]any{"passage_id": "p1", "text": "中文👩‍💻"}}}}},
		{"get_search_content", "Stored content · returned", map[string]any{"responseId": "stored-1", "findText": "中文"}, map[string]any{"responseId": "stored-1", "offset": 0, "nextOffset": nil}},
		{"update_plan", "Plan", map[string]any{"progress": map[string]any{"verification": []any{"中文👩‍💻"}}}, map[string]any{"plan": []any{map[string]any{"id": "verify", "goal": "中文👩‍💻", "status": "in_progress", "extra": "STEP_UNKNOWN"}}, "task_status": "active"}},
		{"web_enable", "Web access", nil, map[string]any{"enabled": []any{"web_search"}, "unavailable": []any{"中文👩‍💻"}}},
		{"obs_recall", "Stored observation", map[string]any{"id": "obs-1"}, map[string]any{"id": "obs-1", "offset": 0, "nextOffset": 900, "bytes": 900, "lines": 4, "eof": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.details["future"] = map[string]any{"exact": json.Number("900719925474099312345"), "text": "UNKNOWN_END"}
			item := visualItem("functions."+tt.name, tt.args, tt.details, payload)
			before, _ := json.Marshal(item)
			if got := inspectorText(item, 60); !strings.Contains(got, tt.heading) {
				t.Fatalf("missing dedicated renderer %q: %s", tt.heading, got)
			}
			for _, width := range []int{1, 2, 20, 40, 60} {
				rows := ToolResultRows(item, testTheme(), width)
				var joined strings.Builder
				for _, row := range rows {
					if ansi.StringWidth(row) > width {
						t.Fatalf("width %d overflow: %q", width, row)
					}
					joined.WriteString(ansi.Strip(row))
				}
				for _, want := range []string{strings.Repeat("LONG_CONTENT_", 40) + "PAYLOAD_END", "UNKNOWN_END", "900719925474099312345"} {
					if !strings.Contains(joined.String(), want) {
						t.Fatalf("width %d lost content %q", width, want)
					}
				}
				again := ToolResultRows(item, testTheme(), width)
				if strings.Join(rows, "\n") != strings.Join(again, "\n") {
					t.Fatal("rendering retained mutable state")
				}
			}
			after, _ := json.Marshal(item)
			if string(before) != string(after) {
				t.Fatal("rendering mutated original item")
			}
			item.Error = true
			item.Running = true
			if got := inspectorText(item, 60); !strings.HasPrefix(got, "Tool failed\n") || !strings.Contains(got, "UNKNOWN_END") {
				t.Fatalf("lost error priority or metadata: %s", got)
			}
		})
	}
}

func TestInspectorFailureAndStreamingSections(t *testing.T) {
	item := visualItem("bash", map[string]any{"command": "exit 2"}, map[string]any{"exitCode": 2}, "failure reason")
	item.Error = true
	text := inspectorText(item, 80)
	if !strings.HasPrefix(text, "Tool failed\nfailure reason") || !strings.Contains(text, "exit 2") || !strings.Contains(text, "✗ Exit code · 2") {
		t.Fatal(text)
	}
	item = visualItem("write", map[string]any{"path": "stream.go", "content": "partial"}, nil, "")
	item.Running = true
	item.HasResult = false
	if text := inspectorText(item, 80); !strings.Contains(text, "writing stream.go") || strings.Contains(text, "written") || !strings.Contains(text, "partial") {
		t.Fatal(text)
	}
	item.Running = false
	item.HasResult = true
	if !strings.Contains(inspectorText(item, 80), "written stream.go") {
		t.Fatal("missing final state")
	}
	item = visualItem("custom", nil, nil, "")
	for _, raw := range []string{`["UNUSUAL",{"future":1}]`, `"UNUSUAL"`, `not-json-UNUSUAL`} {
		item.Details = json.RawMessage(raw)
		if !strings.Contains(inspectorText(item, 80), "UNUSUAL") {
			t.Fatal("lost non-object details")
		}
	}
}
