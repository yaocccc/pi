package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestWorkerTaskSummaryRowsAreIsolatedAndCompact(t *testing.T) {
	for _, result := range []string{
		`{"summary":["SUMMARY_END"],"validation":[{"command":"test","result":"passed"}],"risks":["RISK_END"],"execution":{"session":"PRIVATE_PATH"}}`,
		`"{\"summary\":[\"SUMMARY_END\"],\"validation\":[{\"command\":\"test\",\"result\":\"passed\"}],\"risks\":[\"RISK_END\"]}"`,
	} {
		details := `{"kind":"worker-ui","tasks":[{"title":"任务 🧪","objective":"GOAL_END","status":"completed","preset":"deep","activities":[{"id":"SECRET_ACTIVITY","at":123}],"allowedPaths":["PRIVATE_PATH"],"transcript":{"id":"SECRET_ID"},"result":` + result + `}]}`
		item := session.Item{Role: "tool", ToolName: "worker", Details: json.RawMessage(details), Text: "SECRET_TEXT", Arguments: json.RawMessage(`{"tasks":[{"objective":"OTHER_TASK"}]}`)}
		for _, width := range []int{1, 20, 60} {
			rows := WorkerTaskSummaryRows(item, "记录文件不存在或已清理", testTheme(), width)
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("row exceeded %d: %q", width, row)
				}
			}
			if width < 60 {
				continue
			}
			plain := ansi.Strip(strings.Join(rows, "\n"))
			for _, want := range []string{"简要详情", "GOAL_END", "SUMMARY_END", "passed", "RISK_END", "deep"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("missing %q: %s", want, plain)
				}
			}
			for _, hidden := range []string{"SECRET", "PRIVATE", "OTHER_TASK", "Activity", "transcript"} {
				if strings.Contains(plain, hidden) {
					t.Fatalf("leaked %q", hidden)
				}
			}
		}
		if string(item.Details) != details || item.Text != "SECRET_TEXT" {
			t.Fatal("summary mutated saved item")
		}
	}
}

func TestConversationDrilldownCompleteToolBodies(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"path": "large.go", "content": "FIRST_SOURCE\n" + strings.Repeat("var word = \"完整 👩‍💻\"\n", 100) + "LAST_SOURCE"})
	items := []session.Item{
		{ID: "w", Role: "tool", ToolName: "write", Arguments: args, HasResult: true},
		{ID: "r", Role: "tool", ToolName: "read", Text: "FIRST_RESULT\n" + strings.Repeat("middle 👩‍💻\n", 100) + "LAST_RESULT", HasResult: true},
	}
	for _, width := range []int{1, 2, 20, 40, 60} {
		var rows []string
		for _, item := range items {
			rows = append(rows, ToolResultRows(item, testTheme(), width)...)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("width %d: %q", width, row)
			}
		}
		if width < 40 {
			continue
		}
		all := ansi.Strip(strings.Join(rows, "\n"))
		for _, word := range []string{"FIRST_SOURCE", "LAST_SOURCE", "FIRST_RESULT", "LAST_RESULT"} {
			if !strings.Contains(all, word) {
				t.Fatalf("width %d missing %q", width, word)
			}
		}
	}
}

func TestWorkerTaskResultStableIdentityAndLegacy(t *testing.T) {
	item := session.Item{ID: "parent", Role: "tool", ToolName: "worker", Running: true, Details: json.RawMessage(`{"kind":"worker-ui","batchId":"b","tasks":[{"index":7,"objective":"chosen","unknown":{"long":"KEEP_ME"},"status":"failed","result":{"error":"TASK_ERROR"}},{"index":8,"objective":"secret"}],"controlErrors":[{"message":"OTHER_TASK"}]}`)}
	selected, title, ok := WorkerTaskResult(item, "index:b:7")
	if !ok || title != "chosen" {
		t.Fatal("native task not found")
	}
	for _, width := range []int{1, 2, 20, 40, 60} {
		for _, row := range ToolResultRows(selected, testTheme(), width) {
			if ansi.StringWidth(row) > width {
				t.Fatalf("task width %d: %q", width, row)
			}
		}
	}
	text := ansi.Strip(strings.Join(ToolResultRows(selected, testTheme(), 80), "\n"))
	if !strings.Contains(text, "KEEP_ME") || !strings.Contains(text, "TASK_ERROR") || strings.Contains(text, "secret") || strings.Contains(text, "OTHER_TASK") {
		t.Fatalf("bad isolation: %s", text)
	}
	item.Details = json.RawMessage(`{"kind":"worker-ui","batchId":"b","tasks":[{"index":7,"objective":"ambiguous"},{"index":7,"objective":"also ambiguous"}]}`)
	if _, _, ok := WorkerTaskResult(item, "index:b:7"); ok {
		t.Fatal("duplicate stable key selected arbitrary task")
	}
	item.Details = nil
	item.Arguments = json.RawMessage(`{"tasks":[{"objective":"legacy A","title":"Chosen"},{"objective":"legacy B"}]}`)
	item.Text = `{"tasks":[{"taskId":"stable-a","status":"completed"},{"taskId":"stable-b","status":"completed"}],"result":{"results":[{"summary":["A_RESULT"]},{"summary":["B_SECRET"]}]}}`
	selected, title, ok = WorkerTaskResult(item, "id:stable-a")
	if !ok || title != "Chosen" {
		t.Fatal("legacy identity not resolved")
	}
	text = ansi.Strip(strings.Join(ToolResultRows(selected, testTheme(), 80), "\n"))
	if !strings.Contains(text, "A_RESULT") || !strings.Contains(text, "legacy A") || strings.Contains(text, "B_SECRET") || strings.Contains(text, "legacy B") {
		t.Fatalf("legacy isolation failed: %s", text)
	}
}

func TestToolResultUnabridgedAndSafe(t *testing.T) {
	long := strings.Repeat("a", 130) + "LAST_COLUMN"
	item := visualItem("write", map[string]any{"path": "file.go", "content": "FIRST_LINE\n" + strings.Repeat("middle\n", 30) + long}, nil, "written successfully")
	for _, width := range []int{1, 8, 45, 120} {
		rows := ToolResultRows(item, testTheme(), width)
		text := ansi.Strip(strings.Join(rows, "\n"))
		var content strings.Builder
		for _, row := range strings.Split(text, "\n") {
			if at := strings.Index(row, "│ "); at >= 0 {
				row = row[at+len("│ "):]
			}
			content.WriteString(row)
		}
		if !strings.Contains(content.String(), "FIRST_LINE") || !strings.Contains(content.String(), "LAST_COLUMN") {
			t.Fatalf("truncated full tool at %d", width)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("overflow %d", width)
			}
		}
	}
	hostile := visualItem("bash", nil, nil, "before\x1b]52;c;ATTACK\a\x1b[2Jafter")
	output := strings.Join(ToolResultRows(hostile, testTheme(), 40), "\n")
	if strings.Contains(output, "\x1b]52") || strings.Contains(output, "\x1b[2J") {
		t.Fatal("terminal injection")
	}
}
func TestToolResultDoesNotWrapSyntheticPadding(t *testing.T) {
	// Coloring a multiline block pads shorter lines to the longest line.
	// At inspector widths those spaces must not become empty output rows.
	text := "short\n" + strings.Repeat("x", 90) + "\n\n  tail  "
	item := visualItem("bash", nil, nil, text)
	rows := ToolResultRows(item, testTheme(), 30)
	var got []string
	for _, row := range rows {
		got = append(got, ansi.Strip(row))
	}
	want := []string{"Output", "short", strings.Repeat("x", 30), strings.Repeat("x", 30), strings.Repeat("x", 30), "", "  tail  "}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected padding/blank rows: got %#v, want %#v", got, want)
	}
}

func TestToolResultPreservesSourceWhitespaceAcrossWidths(t *testing.T) {
	source := "短行\n\n  " + strings.Repeat("中文👩‍💻", 12) + "\n\tindented\n \nlast\n"
	for _, failed := range []bool{false, true} {
		for _, width := range []int{1, 8, 18, 30, 58} {
			item := visualItem("bash", nil, nil, source)
			item.Error = failed
			got := ToolResultRows(item, testTheme(), width)
			var want []string
			if failed {
				want = append(want, visualWrap("Tool failed", width)...)
			} else {
				want = append(want, visualWrap("Output", width)...)
			}
			for _, line := range strings.Split(cleanCode(source), "\n") {
				want = append(want, visualWrap(line, width)...)
			}
			if ansi.Strip(strings.Join(got, "\n")) != strings.Join(want, "\n") {
				t.Fatalf("whitespace changed at width %d, failed=%v", width, failed)
			}
		}
	}
}

func TestToolResultDetailsWithoutPaddingRows(t *testing.T) {
	item := session.Item{ToolName: "worker", Details: json.RawMessage(`{"summary":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","count":1}`)}
	rows := ToolResultRows(item, testTheme(), 30)
	for _, row := range rows {
		if strings.TrimSpace(ansi.Strip(row)) == "" {
			t.Fatal("JSON details produced a synthetic blank row")
		}
	}
	text := ansi.Strip(strings.Join(rows, ""))
	if !strings.Contains(text, strings.Repeat("x", 90)) || !strings.Contains(text, `count: 1`) {
		t.Fatal("structured content lost")
	}
}

func TestToolResultEditWorkerAndError(t *testing.T) {
	item := visualItem("edit", map[string]any{"path": "x.go", "oldText": "GUESSED", "newText": "GUESS_NEW"}, map[string]any{"diff": "-1 old value\n+1 AUTHORITATIVE"}, "done")
	text := ansi.Strip(strings.Join(ToolResultRows(item, testTheme(), 40), "\n"))
	if !strings.Contains(text, "AUTHORITATIVE") || strings.Contains(text, "GUESSED") {
		t.Fatal("ignored actual diff")
	}
	item.Details = nil
	text = ansi.Strip(strings.Join(ToolResultRows(item, testTheme(), 80), "\n"))
	if !strings.Contains(text, "not a recorded diff") || !strings.Contains(text, "GUESSED") || !strings.Contains(text, "GUESS_NEW") {
		t.Fatal("missing preview semantics")
	}
	item.Error = true
	item.Text = "ERROR_MESSAGE"
	text = ansi.Strip(strings.Join(ToolResultRows(item, testTheme(), 40), "\n"))
	if !strings.Contains(text, "ERROR_MESSAGE") || strings.Contains(text, "GUESSED") {
		t.Fatal("error not prioritized")
	}
	worker := session.Item{ToolName: "worker", Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"title":"Inspect source","result":{"summary":["RESULT_MARKER"]}}]}`)}
	text = ansi.Strip(strings.Join(ToolResultRows(worker, testTheme(), 80), "\n"))
	if !strings.Contains(text, "RESULT_MARKER") || !strings.Contains(text, "Inspect source") {
		t.Fatal("missing structured worker result")
	}
}

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
