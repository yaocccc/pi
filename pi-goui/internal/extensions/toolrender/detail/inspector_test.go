package detail_test

import (
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender"
	"pi-goui/internal/extensions/toolrender/detail"
	"strings"
	"testing"
)

func testTheme() config.Theme {
	return config.Theme{Accent: "#b7915f", Border: "#756550", Muted: "#948672", Text: "#e0d4bd", Background: "#25231f", UserBackground: "#30342b", AssistantBackground: "#292720", ToolBackground: "#292720", Success: "#a4ad81", Warning: "#d7ae72", Error: "#c77965", Thinking: "#a79b87"}
}
func inspectorText(item session.Item, width int) string {
	return ansi.Strip(strings.Join(render.ToolResultRows(item, testTheme(), width, toolrender.NewViews()), "\n"))
}
func visualItem(name string, args, details any, text string) session.Item {
	a, _ := json.Marshal(args)
	d, _ := json.Marshal(details)
	return session.Item{Role: "tool", ToolName: name, Arguments: a, Details: d, Text: text, HasResult: true}
}
func TestWorkerTaskSummaryRowsAreIsolatedAndCompact(t *testing.T) {
	for _, result := range []string{
		`{"summary":["SUMMARY_END"],"validation":[{"command":"test","result":"passed"}],"risks":["RISK_END"],"execution":{"session":"PRIVATE_PATH"}}`,
		`"{\"summary\":[\"SUMMARY_END\"],\"validation\":[{\"command\":\"test\",\"result\":\"passed\"}],\"risks\":[\"RISK_END\"]}"`,
	} {
		details := `{"kind":"worker-ui","tasks":[{"title":"任务 🧪","objective":"GOAL_END","status":"completed","preset":"deep","activities":[{"id":"SECRET_ACTIVITY","at":123}],"allowedPaths":["PRIVATE_PATH"],"transcript":{"id":"SECRET_ID"},"result":` + result + `}]}`
		item := session.Item{Role: "tool", ToolName: "worker", Details: json.RawMessage(details), Text: "SECRET_TEXT", Arguments: json.RawMessage(`{"tasks":[{"objective":"OTHER_TASK"}]}`)}
		for _, width := range []int{1, 20, 60} {
			rows := detail.WorkerTaskSummaryRows(item, "记录文件不存在或已清理", testTheme(), width, render.StyledLines, render.Clean, func(item session.Item, theme config.Theme, width int) []string {
				return render.ToolResultRows(item, theme, width, toolrender.NewViews())
			})
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

func TestWorkerTaskResultStableIdentityAndLegacy(t *testing.T) {
	item := session.Item{ID: "parent", Role: "tool", ToolName: "worker", Running: true, Details: json.RawMessage(`{"kind":"worker-ui","batchId":"b","tasks":[{"index":7,"objective":"chosen","unknown":{"long":"KEEP_ME"},"status":"failed","result":{"error":"TASK_ERROR"}},{"index":8,"objective":"secret"}],"controlErrors":[{"message":"OTHER_TASK"}]}`)}
	selected, title, ok := detail.WorkerTaskResult(item, "index:b:7")
	if !ok || title != "chosen" {
		t.Fatal("native task not found")
	}
	for _, width := range []int{1, 2, 20, 40, 60} {
		for _, row := range render.ToolResultRows(selected, testTheme(), width, toolrender.NewViews()) {
			if ansi.StringWidth(row) > width {
				t.Fatalf("task width %d: %q", width, row)
			}
		}
	}
	text := ansi.Strip(strings.Join(render.ToolResultRows(selected, testTheme(), 80, toolrender.NewViews()), "\n"))
	if !strings.Contains(text, "KEEP_ME") || !strings.Contains(text, "TASK_ERROR") || strings.Contains(text, "secret") || strings.Contains(text, "OTHER_TASK") {
		t.Fatalf("bad isolation: %s", text)
	}
	item.Details = json.RawMessage(`{"kind":"worker-ui","batchId":"b","tasks":[{"index":7,"objective":"ambiguous"},{"index":7,"objective":"also ambiguous"}]}`)
	if _, _, ok := detail.WorkerTaskResult(item, "index:b:7"); ok {
		t.Fatal("duplicate stable key selected arbitrary task")
	}
	item.Details = nil
	item.Arguments = json.RawMessage(`{"tasks":[{"objective":"legacy A","title":"Chosen"},{"objective":"legacy B"}]}`)
	item.Text = `{"tasks":[{"taskId":"stable-a","status":"completed"},{"taskId":"stable-b","status":"completed"}],"result":{"results":[{"summary":["A_RESULT"]},{"summary":["B_SECRET"]}]}}`
	selected, title, ok = detail.WorkerTaskResult(item, "id:stable-a")
	if !ok || title != "Chosen" {
		t.Fatal("legacy identity not resolved")
	}
	text = ansi.Strip(strings.Join(render.ToolResultRows(selected, testTheme(), 80, toolrender.NewViews()), "\n"))
	if !strings.Contains(text, "A_RESULT") || !strings.Contains(text, "legacy A") || strings.Contains(text, "B_SECRET") || strings.Contains(text, "legacy B") {
		t.Fatalf("legacy isolation failed: %s", text)
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
				rows := render.ToolResultRows(item, testTheme(), width, toolrender.NewViews())
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
				again := render.ToolResultRows(item, testTheme(), width, toolrender.NewViews())
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

func TestInspectorSemanticSections(t *testing.T) {
	tests := []struct {
		name          string
		args, details map[string]any
		output        string
		want          []string
	}{
		{"ask_question", map[string]any{"questions": []any{map[string]any{"question": "Which color?", "options": []any{"red", "blue"}}}}, map[string]any{"questions": []any{map[string]any{"question": "Which color?", "answer": "blue", "future": "ANSWER_METADATA"}}}, "selected blue", []string{"Questions", "Which color?", "○ red", "✓ blue", "Answers", "answer: blue", "ANSWER_METADATA"}},
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

func TestInspectorSectionsWidthSafetyAndFullUnknownValues(t *testing.T) {
	long := strings.Repeat("x", 300) + "END_MARKER"
	items := []session.Item{
		visualItem("worker", nil, map[string]any{"tasks": []any{map[string]any{"objective": "中文👩‍💻", "result": map[string]any{"future": long}}}}, ""),
	}
	for _, width := range []int{1, 2, 8, 20, 56, 100} {
		for _, item := range items {
			rows := render.ToolResultRows(item, testTheme(), width, toolrender.NewViews())
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
