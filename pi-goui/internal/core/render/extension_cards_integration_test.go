package render

import (
	"encoding/json"
	"fmt"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/extensions/toolrender/cards"
	"pi-goui/internal/extensions/toolrender/detail"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	frames "pi-goui/internal/core/render/frame"
	"pi-goui/internal/core/session"
)

// These tests exercise host framing, grouping and expansion through the actual
// generic entry points. Extension protocol/formatting tests live with adapters.
func workerFixture() session.Item {
	return session.Item{ID: "w", Role: "tool", ToolName: "worker", HasResult: true, MessageIndex: 7, EstimatedTokens: 6000,
		Text:    `{"status":"failed","summary":["CONTENT MUST NOT OVERRIDE DETAILS"]}`,
		Details: json.RawMessage(`{"kind":"worker-ui","batchId":"batch-demo","snapshotAt":12000,"startedAt":1000,"total":2,"completed":1,"tasks":[{"index":0,"mode":"implement","objective":"Improve worker cards","status":"completed","requestedPreset":"normal","modelId":"provider/requested","sessionMode":"fork","startedAt":1000,"finishedAt":11000,"usage":{"input":1200,"output":300,"turns":2},"activities":[{"id":"read-1","type":"tool","status":"completed","label":"read","detail":"render.go"}],"result":{"status":"completed","summary":["Implemented task cards"],"changed_files":[{"path":"worker.go","kind":"added"}],"validation":[{"command":"go test ./internal/core/render","result":"passed","details":"12 checks"}],"execution":{"actual_model_id":"provider/actual","route_debug":"HIDDEN ROUTING"}}},{"index":1,"mode":"test","objective":"Verify widths 中文","status":"blocked","requestedPreset":"fast","startedAt":10000,"usage":{"input":0,"output":0,"turns":0},"result":{"status":"blocked","summary":["Missing fixture"],"error":{"message":"Fixture unavailable"}}}]}`)}
}

func TestWorkerHeaderDoesNotStretchViewport(t *testing.T) {
	item := workerFixture()
	item.Details = json.RawMessage(strings.ReplaceAll(string(item.Details), "batch-demo", "a7bd3f65-91ae-430d-90f8-230587f3cdf4"))
	for _, width := range []int{40, 80, 120, 240} {
		rows, _ := toolLayout(item, testTheme(), Options{Tools: testToolViews(), ExpandTools: true}, width)
		row := rows[0]
		plain := ansi.Strip(row)
		if ansi.StringWidth(row) > min(width, 80) || strings.Contains(plain, "  ") {
			t.Fatalf("header padded or oversized at width %d: %q", width, plain)
		}
		if !strings.HasSuffix(plain, " ≈6.0k tok") {
			t.Fatal("token badge lost or detached")
		}
	}
}

func TestCollapsedWorkerShowsOnlyHeader(t *testing.T) {
	for _, running := range []bool{false, true} {
		item := workerFixture()
		item.Running = running
		opts := Options{Tools: testToolViews(), Width: 120, Expanded: map[string]bool{"tool-group:" + item.ID: true, item.ID: false}}
		rows, blocks := toolLayout(item, testTheme(), opts, 100)
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

func TestRunningWorkerStaysInCallsGroup(t *testing.T) {
	worker := workerFixture()
	worker.Running = true
	items := []session.Item{{ID: "a", Role: "tool", ToolName: "read"}, worker, {ID: "b", Role: "tool", ToolName: "bash"}}
	opts := Options{Tools: testToolViews(), Width: 120, Expanded: map[string]bool{}}
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
	task := blockByID(t, doc, "worker-task:w:0:0")
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
	if blockByID(t, done, "worker-task:w:0:0").Foldable {
		t.Fatal("completed task can fold")
	}
}

func TestExtensionCardThemeGeometry(t *testing.T) {
	item := session.Item{ID: "w", Role: "tool", ToolName: "worker", Details: json.RawMessage(`{"kind":"worker-ui","tasks":[{"index":0,"mode":"scout","status":"completed","objective":"Read directory only","result":{"summary":["Directory checked","No changes"]}},{"index":1,"mode":"scout","status":"completed","objective":"Check status","result":{"summary":["Clean"]}}]}`)}
	for _, preset := range []string{"", "catppuccin", "tokyo-night", "nord", "rose-pine"} {
		theme := testTheme()
		theme.FramePreset = preset
		for _, width := range []int{80, 160} {
			card, _ := toolLayout(item, theme, Options{Tools: testToolViews(), ExpandTools: true}, width)
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

// Explicit test-only assembly: production providers are supplied by the application.
func testToolViews() *ToolViews {
	return &ToolViews{
		Policy: cards.Policy,
		Layout: func(host extensionapi.CardHost, item session.Item, theme config.Theme, opts extensionapi.CardOptions, width int) ([]string, []extensionapi.Block, bool) {
			return cards.New(host).Layout(item, theme, opts, width)
		},
		Detail: func(host extensionapi.CardHost, item session.Item, theme config.Theme, width int) ([]string, bool) {
			return cards.New(host).Detail(item, theme, width)
		},
		FullDetail: func(host extensionapi.DetailHost, name string, item session.Item, args, detailsMap map[string]any) bool {
			return detail.New(host).Render(name, item, args, detailsMap)
		},
	}
}
