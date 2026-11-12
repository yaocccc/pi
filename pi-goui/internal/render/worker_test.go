package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	frames "pi-goui/internal/render/frame"
	"pi-goui/internal/session"
)

func workerFixture() session.Item {
	return session.Item{ID: "w", Role: "tool", ToolName: "worker", HasResult: true, MessageIndex: 7, EstimatedTokens: 6000,
		Text:    `{"status":"failed","summary":["CONTENT MUST NOT OVERRIDE DETAILS"]}`,
		Details: json.RawMessage(`{"kind":"worker-ui","batchId":"batch-demo","snapshotAt":12000,"startedAt":1000,"total":2,"completed":1,"tasks":[{"index":0,"mode":"implement","objective":"Improve worker cards","status":"completed","requestedPreset":"normal","modelId":"provider/requested","sessionMode":"fork","startedAt":1000,"finishedAt":11000,"usage":{"input":1200,"output":300,"turns":2},"activities":[{"id":"read-1","type":"tool","status":"completed","label":"read","detail":"render.go"}],"result":{"status":"completed","summary":["Implemented task cards"],"changed_files":[{"path":"worker.go","kind":"added"}],"validation":[{"command":"go test ./internal/render","result":"passed","details":"12 checks"}],"execution":{"actual_model_id":"provider/actual","route_debug":"HIDDEN ROUTING"}}},{"index":1,"mode":"test","objective":"Verify widths 中文","status":"blocked","requestedPreset":"fast","startedAt":10000,"usage":{"input":0,"output":0,"turns":0},"result":{"status":"blocked","summary":["Missing fixture"],"error":{"message":"Fixture unavailable"}}}]}`)}
}
func TestWorkerTitleAndObjectiveFallback(t *testing.T) {
	for _, title := range []any{nil, "", " \t ", "修复任务卡片"} {
		task := map[string]any{"mode": "scout", "objective": "Complete execution goal"}
		if title != nil {
			task["title"] = title
		}
		for _, authoritative := range []bool{true, false} {
			item := session.Item{ID: "title", Role: "tool", ToolName: "worker"}
			if authoritative {
				item.Details, _ = json.Marshal(map[string]any{"kind": "worker-ui", "tasks": []any{task}})
			} else {
				item.Arguments, _ = json.Marshal(map[string]any{"tasks": []any{task}})
			}
			rows, _ := workerDetailRows(item, testTheme(), Options{}, 80)
			output := ansi.Strip(strings.Join(rows, "\n"))
			want := "Complete execution goal"
			if title == "修复任务卡片" {
				want = title.(string)
				if strings.Contains(output, "Complete execution goal") {
					t.Fatal("objective shown instead of title")
				}
			}
			if !strings.Contains(output, want) {
				t.Fatalf("title=%v authoritative=%v: %q", title, authoritative, output)
			}
			if workerData(item).cards[0].objective != "Complete execution goal" {
				t.Fatal("objective changed")
			}
		}
	}
}

func TestWorkerHeaderDoesNotStretchViewport(t *testing.T) {
	item := workerFixture()
	view := workerData(item)
	view.batch = "a7bd3f65-91ae-430d-90f8-230587f3cdf4"
	for _, width := range []int{40, 80, 120, 240} {
		row := workerHeader(item, view, true, testTheme(), width)
		plain := ansi.Strip(row)
		if ansi.StringWidth(row) > min(width, 80) || strings.Contains(plain, "  ") {
			t.Fatalf("header padded or oversized at width %d: %q", width, plain)
		}
		if !strings.HasSuffix(plain, " ≈6.0k tok") {
			t.Fatal("token badge lost or detached")
		}
	}
}

func TestWorkerDurationWholeSeconds(t *testing.T) {
	for _, tc := range []struct {
		elapsed float64
		want    string
	}{
		{0, "0s"}, {400, "0s"}, {900, "1s"}, {10400, "10s"}, {11900, "12s"}, {59900, "1m0s"}, {62300, "1m2s"},
	} {
		if got := workerDuration(workerCard{started: 1000, finished: 1000 + tc.elapsed}, 0); got != tc.want {
			t.Fatalf("duration %v = %q, want %q", tc.elapsed, got, tc.want)
		}
	}
	if workerDuration(workerCard{}, 12000) != "" {
		t.Fatal("missing timestamps fabricated duration")
	}
}

func TestWorkerTaskSectionIndentation(t *testing.T) {
	item := session.Item{ID: "w", Role: "tool", ToolName: "worker", Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"scout","status":"completed","objective":"Read directory only","result":{"summary":["Directory checked","No changes"]}},{"index":1,"mode":"scout","status":"completed","objective":"Check status","result":{"summary":["Clean"]}}]}`)}
	rows, blocks := workerDetailRows(item, testTheme(), Options{}, 80)
	want := []string{"✓ scout", "  ◦ Read directory only", "  • Directory checked", "  • No changes", strings.Repeat("─", 23), "✓ scout", "  ◦ Check status", "  • Clean"}
	if len(rows) != len(want) {
		t.Fatalf("unexpected rows: %v", rows)
	}
	for i := range rows {
		if got := ansi.Strip(rows[i]); got != want[i] {
			t.Fatalf("row %d: %q, want %q", i, got, want[i])
		}
	}
	if len(blocks) != 2 || blocks[0].End != 4 || blocks[1].Start != 5 || blocks[0].Foldable || blocks[1].Foldable {
		t.Fatalf("task ranges changed: %+v", blocks)
	}
	for _, preset := range []string{"", "catppuccin", "tokyo-night", "nord", "rose-pine"} {
		theme := testTheme()
		theme.FramePreset = preset
		for _, width := range []int{80, 160} {
			card, _ := workerLayout(item, theme, Options{ExpandTools: true}, width)
			if ansi.StringWidth(card[1]) != 27 {
				t.Fatalf("card stretched to terminal width %d: %q", width, card[1])
			}
			corners, _ := frames.Palette(preset, "worker")
			c := corners[0]
			separator := paint(strings.Repeat("─", 23), fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]))
			if !strings.Contains(card[6], separator) {
				t.Fatal("separator does not use worker border color")
			}
		}
	}
}

func TestWorkerSlotNoticesHidden(t *testing.T) {
	for _, phase := range []string{"等待执行槽位", "等待槽位", "已获得槽位"} {
		for _, status := range []string{"queued", "running"} {
			details, _ := json.Marshal(map[string]any{"kind": "worker-ui", "tasks": []any{map[string]any{
				"mode": "scout", "objective": "Inspect", "status": status, "phase": phase,
				"activities": []any{map[string]any{"type": "phase", "label": phase, "status": "running"}},
			}}})
			item := session.Item{ID: "w", Role: "tool", ToolName: "worker", Running: true, Details: details}
			rows, _ := workerDetailRows(item, testTheme(), Options{}, 80)
			if strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "槽位") {
				t.Fatal("slot notice leaked")
			}
		}
	}
	for _, failure := range []string{"等待执行槽位超时", "Worker 在等待执行槽位时已取消"} {
		if workerSlotNotice(failure) {
			t.Fatal("failure must not be hidden")
		}
	}
}

func TestWorkerStepsInterleaveToolsAndThinking(t *testing.T) {
	item := session.Item{ID: "w", Role: "tool", ToolName: "worker", Running: true, Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"status":"running","mode":"scout","objective":"Inspect source","modelId":"hidden/model","sessionMode":"fork","activities":[{"type":"thinking","label":"思考","detail":"First explanation"},{"type":"tool","status":"running","label":"read","detail":"secret.go"},{"type":"thinking","label":"思考","detail":"Second explanation"},{"type":"phase","label":"等待槽位"},{"type":"phase","label":"已获得槽位"}]}]}`)}
	opts := Options{Expanded: map[string]bool{workerTaskID(item, 0, 0): false}}
	rows, blocks := workerDetailRows(item, testTheme(), opts, 100)
	text := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"Inspect source", "≡ Activity", "⋯ read secret.go (运行中…)", "First explanation", "Second explanation"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Index(text, "First explanation") > strings.Index(text, "secret.go") || strings.Index(text, "secret.go") > strings.Index(text, "Second explanation") {
		t.Fatal("reordered progress")
	}
	if strings.Count(text, "First explanation") != 1 || strings.Count(text, "Second explanation") != 1 || strings.Contains(text, "正在执行中") {
		t.Fatal("thinking duplicated below steps")
	}
	for _, hidden := range []string{"hidden/model", "fork", "等待槽位", "已获得槽位"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("unexpected %q: %s", hidden, text)
		}
	}
	if len(blocks) != 1 || blocks[0].Foldable || !blocks[0].Expanded {
		t.Fatalf("task can fold: %+v", blocks)
	}
}

func TestWorkerDetailsAuthoritativeAndTaskFolds(t *testing.T) {
	item := workerFixture()
	opts := Options{Width: 120, Expanded: map[string]bool{"tool-group:" + item.ID: true, item.ID: true}}
	doc := Transcript([]session.Item{item}, testTheme(), opts)
	plain := ansi.Strip(doc.Text)
	for _, want := range []string{"worker · 2 tasks · blocked", "implement · normal · 2 turns", "test · fast", "• Implemented task cards", "Implemented task cards", "Missing fixture", "↑1k ↓300", "10s", "2s", "Improve worker cards"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	for _, hidden := range []string{"CONTENT MUST NOT", "HIDDEN ROUTING", "Fixture unavailable", "provider/actual", "fixture/model", "fork", "new"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("fold/detail leak: %s", hidden)
		}
	}
	for _, hidden := range []string{"worker.go", "go test ./internal/render", "12 checks"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("detailed result leaked: %s", hidden)
		}
	}
	parent := blockByID(t, doc, item.ID)
	if parent.End <= parent.Start+1 || !parent.Expanded {
		t.Fatalf("expanded worker body should toggle: %+v", parent)
	}
	for _, b := range doc.Blocks {
		if b.Role == "workerTask" && (b.Foldable || !b.Expanded) {
			t.Fatal("task must always be expanded and non-foldable")
		}
	}
	opts.Expanded[item.ID] = false
	collapsed := Transcript([]session.Item{item}, testTheme(), opts)
	if blockByID(t, collapsed, item.ID).Expanded || strings.Contains(ansi.Strip(collapsed.Text), "Implemented task cards") {
		t.Fatal("card did not collapse")
	}
}
func TestCollapsedWorkerShowsOnlyHeader(t *testing.T) {
	for _, running := range []bool{false, true} {
		item := workerFixture()
		item.Running = running
		opts := Options{Width: 120, Expanded: map[string]bool{"tool-group:" + item.ID: true, item.ID: false}}
		rows, blocks := workerLayout(item, testTheme(), opts, 100)
		if len(rows) != 1 || len(blocks) != 1 || blocks[0].Expanded {
			t.Fatalf("collapsed Worker leaked task previews: %v, %+v", rows, blocks)
		}
		doc := Transcript([]session.Item{item}, testTheme(), opts)
		plain := ansi.Strip(doc.Text)
		for _, hidden := range []string{"implement", "Improve worker cards", "Implemented task cards", "Verify widths"} {
			if strings.Contains(plain, hidden) {
				t.Fatalf("collapsed task detail leaked: %s", hidden)
			}
		}
		opts.Expanded[item.ID] = true
		if !strings.Contains(ansi.Strip(Transcript([]session.Item{item}, testTheme(), opts).Text), "Implemented task cards") {
			t.Fatal("expanded Worker lost task details")
		}
	}
}

func TestWorkerPartialSnapshotAndCompactPreview(t *testing.T) {
	item := workerFixture()
	item.Running = true
	item.Details = json.RawMessage(`{"kind":"worker-ui","snapshotAt":5000,"controlErrors":[{"message":"Cancel was rejected"}],"tasks":[{"index":4,"status":"running","mode":"scout","objective":"Inspect source","requestedPreset":"fast","startedAt":1000,"phase":"Reading source","activities":[{"type":"tool","status":"running","label":"read","detail":"src/main.go"}]}]}`)
	opts := Options{Width: 100}
	compact := Transcript([]session.Item{item}, testTheme(), opts)
	if len(compact.Blocks) != 1 || compact.Blocks[0].Expanded || strings.Contains(ansi.Strip(compact.Text), "Inspect source") || strings.Contains(ansi.Strip(compact.Text), "╭─ worker ") {
		t.Fatal("collapsed running task leaked preview")
	}
	opts.ExpandTools = true
	first := Transcript([]session.Item{item}, testTheme(), opts)
	second := Transcript([]session.Item{item}, testTheme(), opts)
	if first.Text != second.Text {
		t.Fatal("history snapshot duration drifted")
	}
	for _, s := range []string{"scout · fast · 4s", "Cancel was rejected", "≡ Activity"} {
		if !strings.Contains(ansi.Strip(first.Text), s) {
			t.Fatal(s)
		}
	}
}
func TestRunningWorkerStaysInCallsGroup(t *testing.T) {
	worker := workerFixture()
	worker.Running = true
	items := []session.Item{{ID: "a", Role: "tool", ToolName: "read"}, worker, {ID: "b", Role: "tool", ToolName: "bash"}}
	opts := Options{Width: 120, Expanded: map[string]bool{}}
	doc := Transcript(items, testTheme(), opts)
	groups := 0
	for _, b := range doc.Blocks {
		if b.Role == "toolGroup" {
			groups++
		}
	}
	if groups != 1 || !strings.Contains(ansi.Strip(doc.Text), "3 calls") {
		t.Fatalf("worker outside group: %+v", doc.Blocks)
	}
	if len(doc.Blocks) != 1 || blockByID(t, doc, "tool-group:a").Expanded {
		t.Fatal("live worker must not auto-open its group")
	}
	opts.Expanded["tool-group:a"] = true
	doc = Transcript(items, testTheme(), opts)
	if blockByID(t, doc, "w").Expanded || strings.Contains(ansi.Strip(doc.Text), "Implemented task cards") {
		t.Fatal("opening outer group must not auto-open Worker")
	}
	opts.Expanded["w"] = true
	doc = Transcript(items, testTheme(), opts)
	task := blockByID(t, doc, workerTaskID(worker, 0, 0))
	if task.Foldable || !task.Expanded {
		t.Fatal("task has fold state")
	}
	opts.Expanded[task.ID] = false
	if !strings.Contains(ansi.Strip(Transcript(items, testTheme(), opts).Text), "Implemented task cards") {
		t.Fatal("obsolete task fold still hides conclusion")
	}
	opts.Expanded["tool-group:a"] = false
	for _, global := range []bool{false, true} {
		opts.ExpandTools = global
		collapsed := Transcript(items, testTheme(), opts)
		if len(collapsed.Blocks) != 1 || collapsed.Blocks[0].Expanded || strings.Contains(ansi.Strip(collapsed.Text), "╭─ worker ") || strings.Contains(ansi.Strip(collapsed.Text), "Implemented task cards") {
			t.Fatal("outer fold must hide every child even with global expansion")
		}
	}
	opts.ExpandTools = false
	delete(opts.Expanded, "tool-group:a")
	items[1].Running = false
	done := Transcript(items, testTheme(), opts)
	if len(done.Blocks) != 1 || done.Blocks[0].ID != "tool-group:a" {
		t.Fatal("completion changed group identity")
	}
	opts.ExpandTools = true
	done = Transcript(items, testTheme(), opts)
	if blockByID(t, done, workerTaskID(worker, 0, 0)).Foldable {
		t.Fatal("completed task can fold")
	}
}

func TestWorkerFallbackEnvelopesAndUnavailableMetrics(t *testing.T) {
	item := session.Item{ID: "legacy", Role: "tool", ToolName: "worker", HasResult: true, Arguments: json.RawMessage(`{"batchId":"saved-batch"}`), Text: `{"batchId":"saved-batch","status":"completed","tasks":[{"taskId":"a","status":"blocked"}],"result":{"results":[{"status":"blocked","summary":["Need authorization"],"execution":{"resolved_preset":"deep","actual_model_id":"fixture/model","usage":{"input":42,"output":17,"turns":1}}}]}}`}
	doc := Transcript([]session.Item{item}, testTheme(), Options{Width: 100, ExpandTools: true})
	plain := ansi.Strip(doc.Text)
	for _, s := range []string{"blocked", "saved-batch", "deep", "↑42 ↓17", "Need authorization"} {
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
	if !strings.Contains(plain, "queued") || strings.Contains(plain, "1 turns") || strings.Contains(plain, "0ms") {
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
