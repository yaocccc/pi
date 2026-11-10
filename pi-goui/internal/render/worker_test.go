package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func workerFixture() session.Item {
	return session.Item{ID: "w", Role: "tool", ToolName: "worker", HasResult: true, MessageIndex: 7, EstimatedTokens: 6000,
		Text:    `{"status":"failed","summary":["CONTENT MUST NOT OVERRIDE DETAILS"]}`,
		Details: json.RawMessage(`{"kind":"worker-ui","batchId":"batch-demo","snapshotAt":12000,"startedAt":1000,"total":2,"completed":1,"tasks":[{"index":0,"mode":"implement","objective":"Improve worker cards","status":"completed","requestedPreset":"normal","modelId":"provider/requested","sessionMode":"fork","startedAt":1000,"finishedAt":11000,"usage":{"input":1200,"output":300,"turns":2},"activities":[{"id":"read-1","type":"tool","status":"completed","label":"read","detail":"render.go"}],"result":{"status":"completed","summary":["Implemented task cards"],"changed_files":[{"path":"worker.go","kind":"added"}],"validation":[{"command":"go test ./internal/render","result":"passed","details":"12 checks"}],"execution":{"actual_model_id":"provider/actual","route_debug":"HIDDEN ROUTING"}}},{"index":1,"mode":"test","objective":"Verify widths 中文","status":"blocked","requestedPreset":"fast","startedAt":10000,"usage":{"input":0,"output":0,"turns":0},"result":{"status":"blocked","summary":["Missing fixture"],"error":{"message":"Fixture unavailable"}}}]}`)}
}
func TestWorkerToolActivityUsesNormalToolColors(t *testing.T) {
	theme := testTheme()
	theme.Text, theme.Dim, theme.Success, theme.Error, theme.Warning = "#abcdef", "#778899", "#11aa22", "#dd2233", "#eeaa11"
	for _, status := range []string{"completed", "failed", "running"} {
		item := session.Item{ID: "w", Role: "tool", ToolName: "worker", Running: true}
		item.Details = json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"status":"running","mode":"scout","activities":[{"type":"tool","status":"` + status + `","label":"read","detail":"src/main.go"}]}]}`)
		rows, _ := workerDetailRows(item, theme, Options{}, 100)
		var row string
		for _, candidate := range rows {
			if strings.Contains(ansi.Strip(candidate), "src/main.go") {
				row = candidate
				break
			}
		}
		icon, color := workerStatus(status, theme)
		if status == "running" {
			icon = "→"
		}
		for _, want := range []string{paint(icon, color), paint("read", theme.Text), paint("src/main.go", visualDim(theme))} {
			if !strings.Contains(row, want) {
				t.Fatalf("%s: unexpected activity styling: %q, missing %q", status, row, want)
			}
		}
	}
}

func TestWorkerDetailsAuthoritativeAndTaskFolds(t *testing.T) {
	item := workerFixture()
	opts := Options{Width: 120, Expanded: map[string]bool{"tool-group:" + item.ID: true, item.ID: true}}
	doc := Transcript([]session.Item{item}, testTheme(), opts)
	plain := ansi.Strip(doc.Text)
	for _, want := range []string{"worker · 2 tasks · blocked", "implement · normal · provider/actual · fork · Turn 2", "test · fast", "结果:", "Implemented task cards", "Missing fixture", "↑1.2k ↓300", "10.0s", "2.0s", "Improve worker cards"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	for _, hidden := range []string{"CONTENT MUST NOT", "HIDDEN ROUTING", "Fixture unavailable"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("fold/detail leak: %s", hidden)
		}
	}
	for _, hidden := range []string{"read · render.go", "worker.go", "go test ./internal/render", "12 checks"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("detailed result leaked: %s", hidden)
		}
	}
	parent := blockByID(t, doc, item.ID)
	if parent.End <= parent.Start+1 || !parent.Expanded {
		t.Fatalf("card must be one expanded click target: %+v", parent)
	}
	for _, b := range doc.Blocks {
		if b.Role == "workerTask" {
			t.Fatal("completed card has a secondary fold")
		}
	}
	opts.Expanded[item.ID] = false
	collapsed := Transcript([]session.Item{item}, testTheme(), opts)
	if blockByID(t, collapsed, item.ID).Expanded || strings.Contains(ansi.Strip(collapsed.Text), "Implemented task cards") {
		t.Fatal("card did not collapse")
	}
}
func TestWorkerPartialSnapshotAndCompactPreview(t *testing.T) {
	item := workerFixture()
	item.Running = true
	item.Details = json.RawMessage(`{"kind":"worker-ui","snapshotAt":5000,"controlErrors":[{"message":"Cancel was rejected"}],"tasks":[{"index":4,"status":"running","mode":"scout","objective":"Inspect source","requestedPreset":"fast","startedAt":1000,"phase":"Reading source","activities":[{"type":"tool","status":"running","label":"read","detail":"src/main.go"}]}]}`)
	opts := Options{Width: 100}
	compact := Transcript([]session.Item{item}, testTheme(), opts)
	if !strings.Contains(ansi.Strip(compact.Text), "Inspect source") || !strings.Contains(ansi.Strip(compact.Text), "4.0s") {
		t.Fatal("collapsed running task lost preview")
	}
	opts.ExpandTools = true
	first := Transcript([]session.Item{item}, testTheme(), opts)
	second := Transcript([]session.Item{item}, testTheme(), opts)
	if first.Text != second.Text {
		t.Fatal("history snapshot duration drifted")
	}
	for _, s := range []string{"scout · fast · 4.0s", "Cancel was rejected", "→ read · src/main.go"} {
		if !strings.Contains(ansi.Strip(first.Text), s) {
			t.Fatal(s)
		}
	}
}
func TestRunningWorkerOpensOutsideGroupsThenRejoins(t *testing.T) {
	worker := workerFixture()
	worker.Running = true
	worker.Details = json.RawMessage(`{"kind":"worker-ui","snapshotAt":5000,"tasks":[{"index":0,"status":"running","mode":"scout","objective":"Inspect source","requestedPreset":"fast","startedAt":1000,"activities":[{"type":"tool","status":"completed","label":"read","detail":"src/main.go","contextSizeEstimate":1200}]}]}`)
	items := []session.Item{{ID: "a", Role: "tool", ToolName: "read"}, {ID: "b", Role: "tool", ToolName: "bash"}, worker, {ID: "c", Role: "tool", ToolName: "read"}, {ID: "d", Role: "tool", ToolName: "read"}}
	opts := Options{Width: 100, Expanded: map[string]bool{}}
	doc := Transcript(items, testTheme(), opts)
	if !blockByID(t, doc, "w").Expanded || !blockByID(t, doc, workerTaskID(worker, 0, 0)).Expanded {
		t.Fatal("live Worker/task did not open by default")
	}
	for _, id := range []string{"tool-group:a", "tool-group:c"} {
		b := blockByID(t, doc, id)
		if !b.Expanded || b.End-b.Start != 3 {
			t.Fatal("pending groups must retain their automatic previews")
		}
	}
	if len(opts.Expanded) != 0 {
		t.Fatal("automatic expansion must not become explicit fold state")
	}
	if !strings.Contains(ansi.Strip(doc.Text), "read · ≈1.2k tok · src/main.go") {
		t.Fatal(doc.Text)
	}
	opts.Expanded["w"] = false
	if blockByID(t, Transcript(items, testTheme(), opts), "w").Expanded {
		t.Fatal("manual collapse ignored")
	}
	delete(opts.Expanded, "w")
	items[2].Running = false
	// Stale running task details alone must not auto-open a saved invocation.
	done := Transcript(items, testTheme(), opts)
	if len(done.Blocks) != 1 || done.Blocks[0].ID != "tool-group:a" || !done.Blocks[0].Expanded || !strings.Contains(ansi.Strip(done.Text), "5 calls") {
		t.Fatalf("finished Worker did not rejoin: %+v\n%s", done.Blocks, done.Text)
	}
	opts.Expanded["tool-group:a"] = true
	done = Transcript(items, testTheme(), opts)
	if blockByID(t, done, "w").Expanded {
		t.Fatal("automatic live expansion leaked into completed state")
	}
}

func TestWorkerFallbackEnvelopesAndUnavailableMetrics(t *testing.T) {
	item := session.Item{ID: "legacy", Role: "tool", ToolName: "worker", HasResult: true, Arguments: json.RawMessage(`{"batchId":"saved-batch"}`), Text: `{"batchId":"saved-batch","status":"completed","tasks":[{"taskId":"a","status":"blocked"}],"result":{"results":[{"status":"blocked","summary":["Need authorization"],"execution":{"resolved_preset":"deep","actual_model_id":"fixture/model","usage":{"input":42,"output":17,"turns":1}}}]}}`}
	doc := Transcript([]session.Item{item}, testTheme(), Options{Width: 100, ExpandTools: true})
	plain := ansi.Strip(doc.Text)
	for _, s := range []string{"blocked", "saved-batch", "deep", "fixture/model", "↑42 ↓17", "Need authorization"} {
		if !strings.Contains(plain, s) {
			t.Fatalf("missing %q:\n%s", s, plain)
		}
	}
	if strings.Contains(plain, "0ms") || strings.Contains(plain, "✓ worker") {
		t.Fatal("invented metrics or success")
	}
	item.Arguments = json.RawMessage(`{"tasks":[{"mode":"review","objective":"Check safety","preset":"normal"}]}`)
	item.Running = true
	item.Text = ""
	item.HasResult = false
	plain = ansi.Strip(Transcript([]session.Item{item}, testTheme(), Options{Width: 100, ExpandTools: true}).Text)
	if !strings.Contains(plain, "queued") || strings.Contains(plain, "Turn 1") || strings.Contains(plain, "0ms") {
		t.Fatal("arguments-only call invents progress")
	}
	for _, status := range []string{"failed", "cancelled", "blocked"} {
		item.Running = false
		item.Text = `{"status":"` + status + `","error":"Rejected operation"}`
		plain = ansi.Strip(Transcript([]session.Item{item}, testTheme(), Options{Width: 100, ExpandTools: true}).Text)
		if !strings.Contains(plain, status) || !strings.Contains(plain, "Rejected operation") {
			t.Fatal(plain)
		}
	}
	item.Text = "Invalid response\nraw error evidence"
	item.Details = json.RawMessage(`{broken`)
	if !strings.Contains(ansi.Strip(Transcript([]session.Item{item}, testTheme(), Options{Width: 100, ExpandTools: true}).Text), "raw error evidence") {
		t.Fatal("non-JSON evidence lost")
	}
}
func TestWorkerControlPayloadWithoutTasks(t *testing.T) {
	item := session.Item{ID: "cancel", Role: "tool", ToolName: "worker", HasResult: true,
		Arguments: json.RawMessage(`{"batchId":"missing","cancel":true}`),
		Details:   json.RawMessage(`{"kind":"worker-ui","tasks":[],"payload":{"status":"failed","error":"Batch not found"}}`),
		Text:      `{"summary":["UNTRUSTED CONTENT OVERRIDE"]}`}
	text := ansi.Strip(Transcript([]session.Item{item}, testTheme(), Options{Width: 80, ExpandTools: true}).Text)
	if !strings.Contains(text, "failed") || !strings.Contains(text, "Batch not found") || strings.Contains(text, "UNTRUSTED CONTENT") {
		t.Fatal(text)
	}
}

func TestWorkerWidthsSanitizationAndGroupIndent(t *testing.T) {
	item := workerFixture()
	item.Details = json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"review\u001b[2J","objective":"中文👩‍💻 long path /src/a.go","status":"completed","requestedPreset":"normal","modelId":"a/model","result":{"summary":["safe\u001b]52;c;ATTACK\u0007"]}}]}`)
	items := []session.Item{item, {ID: "b", Role: "tool", ToolName: "bash", Text: "hello", HasResult: true}}
	for _, width := range []int{1, 2, 3, 5, 8, 12, 20, 40, 60, 80, 132} {
		doc := Transcript(items, testTheme(), Options{Width: width, ExpandTools: true})
		checkWidth(t, doc.Text, width)
		if strings.Contains(doc.Text, "\x1b[2J") || strings.Contains(doc.Text, "\x1b]52") || strings.Contains(doc.Text, "48;2;") {
			t.Fatalf("unsafe style width=%d", width)
		}
		if width >= 20 {
			lines := strings.Split(ansi.Strip(doc.Text), "\n")
			group := blockByID(t, doc, "tool-group:w")
			child := blockByID(t, doc, "w")
			gx := strings.Index(lines[group.Start], "⌄")
			cx := strings.Index(lines[child.Start], "⌄")
			if cx-gx != 2 {
				t.Fatalf("child indent=%d width=%d", cx-gx, width)
			}
			if strings.Index(lines[child.Start+1], "╭") != cx {
				t.Fatal("result frame not aligned with call")
			}
		}
	}
}
func TestLocalizedTitlesAndModelName(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "hi"}, {ID: "a", Role: "assistant", Text: "hello"}, {ID: "s", Role: "system", Text: "note"}}
	doc := Transcript(items, testTheme(), Options{Width: 80, AssistantTitle: "Model 中文\x1b[2J"})
	plain := ansi.Strip(doc.Text)
	for _, title := range []string{"╭─ 你 ", "╭─ Model 中文 ", "╭─ 系统 "} {
		if !strings.Contains(plain, title) {
			t.Fatal(plain)
		}
	}
	if strings.Contains(doc.Text, "\x1b[2J") {
		t.Fatal("model title injected terminal commands")
	}
}
