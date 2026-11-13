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
