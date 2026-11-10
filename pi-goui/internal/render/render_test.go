package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func testTheme() config.Theme {
	return config.Theme{Accent: "#b7915f", Border: "#756550", Muted: "#948672", Text: "#e0d4bd", Background: "#25231f", UserBackground: "#30342b", AssistantBackground: "#292720", ToolBackground: "#292720", Success: "#a4ad81", Warning: "#d7ae72", Error: "#c77965", Thinking: "#a79b87"}
}
func checkWidth(t *testing.T, s string, width int) {
	t.Helper()
	for i, row := range strings.Split(s, "\n") {
		if n := ansi.StringWidth(row); n > width {
			t.Fatalf("line %d is %d cells, maximum %d: %q", i, n, width, row)
		}
	}
}
func TestTranscriptBubblesMarkdownAndWidths(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "Please render **Markdown** and 中文 👩‍💻."}, {ID: "a", Role: "assistant", Text: "# Heading\n\n- a **strong** answer\n- a [link](https://example.com)\n\n```go\nfunc main() { println(\"a very very very long unbroken code string 中文👩‍💻\") }\n```\n\n| Key | Value |\n|---|---|\n| 中文 | aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa |"}}
	for _, width := range []int{8, 16, 20, 40, 80, 132} {
		t.Run(string(rune(width)), func(t *testing.T) {
			doc := Transcript(items, testTheme(), Options{Width: width})
			checkWidth(t, doc.Text, width)
			if len(doc.Blocks) != 2 {
				t.Fatalf("blocks=%+v", doc.Blocks)
			}
			plain := ansi.Strip(doc.Text)
			if strings.Contains(plain, "```go") || strings.Contains(plain, "**strong**") {
				t.Fatal("raw Markdown leaked")
			}
			if width >= 40 && (!strings.Contains(plain, "你") || !strings.Contains(plain, "助手") || !strings.Contains(plain, "Heading")) {
				t.Fatalf("missing bubble/Markdown label: %s", plain)
			}
		})
	}
}
func TestToolsFoldOffsetsAndBashPreview(t *testing.T) {
	output := strings.Repeat("line 中文 long output\n", 1200) + "FINAL_RESULT_MARKER"
	items := []session.Item{{ID: "a", Role: "assistant", Text: "Before tool"}, {ID: "tool1", Role: "tool", ToolName: "bash", Arguments: json.RawMessage(`{"command":"ls -l"}`), Text: output}, {ID: "b", Role: "assistant", Text: "After tool"}}
	collapsed := Transcript(items, testTheme(), Options{Width: 70})
	if strings.Contains(collapsed.Text, "FINAL_RESULT_MARKER") {
		t.Fatal("default output not folded")
	}
	if len(collapsed.Blocks) != 3 {
		t.Fatalf("blocks %+v", collapsed.Blocks)
	}
	b := collapsed.Blocks[1]
	if b.ID != "tool-group:tool1" || !b.Foldable || b.End-b.Start != 1 || !strings.Contains(ansi.Strip(collapsed.Text), "1 calls") {
		t.Fatalf("tool not single row: %+v", b)
	}
	rows := strings.Split(ansi.Strip(collapsed.Text), "\n")
	if !strings.Contains(rows[b.Start], "bash") {
		t.Fatalf("bad tool offset: %+v", b)
	}
	if strings.Count(ansi.Strip(collapsed.Text), "助手") != 1 {
		t.Fatal("assistant turn was not grouped")
	}
	expanded := Transcript(items, testTheme(), Options{Width: 70, Expanded: map[string]bool{"tool-group:tool1": true, "tool1": true}})
	if strings.Contains(ansi.Strip(expanded.Text), "FINAL_RESULT_MARKER") || !strings.Contains(ansi.Strip(expanded.Text), "(truncated)") {
		t.Fatal("expanded bash must show only the output head")
	}
	if items[1].Text != output {
		t.Fatal("preview changed saved output")
	}
	checkWidth(t, expanded.Text, 70)
	global := Transcript(items, testTheme(), Options{Width: 70, ExpandTools: true, Expanded: map[string]bool{"tool1": false}})
	if strings.Contains(global.Text, "FINAL_RESULT_MARKER") {
		t.Fatal("per-tool false must override global expand")
	}
	for _, block := range expanded.Blocks {
		if block.Start < 0 || block.End > len(strings.Split(expanded.Text, "\n")) || block.End <= block.Start {
			t.Fatalf("invalid range %+v", block)
		}
	}
}
func TestConcurrentMarkdownRendering(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			doc := Transcript([]session.Item{{ID: "code", Role: "assistant", Text: "```go\nfunc main() { println(42) }\n```"}}, testTheme(), Options{Width: 40})
			checkWidth(t, doc.Text, 40)
			if !strings.Contains(ansi.Strip(doc.Text), "println(42)") {
				t.Fatal("code content missing")
			}
		})
	}
}

func TestThinkingHiddenWithoutMutation(t *testing.T) {
	items := []session.Item{{ID: "a", Role: "assistant", Text: "Visible answer", Thinking: "PRIVATE_REASONING_MARKER"}, {ID: "t", Role: "thinking", Text: "SECOND_THOUGHT_MARKER"}}
	hidden := Transcript(items, testTheme(), Options{Width: 80})
	if strings.Contains(hidden.Text, "REASONING") || strings.Contains(hidden.Text, "SECOND_THOUGHT") {
		t.Fatal("thinking exposed")
	}
	shown := Transcript(items, testTheme(), Options{Width: 80, ShowThinking: true})
	if !strings.Contains(shown.Text, "PRIVATE_REASONING_MARKER") || !strings.Contains(shown.Text, "SECOND_THOUGHT_MARKER") {
		t.Fatal("thinking missing")
	}
	if items[0].Thinking != "PRIVATE_REASONING_MARKER" {
		t.Fatal("input mutated")
	}
}
func TestTerminalControlsCannotEscape(t *testing.T) {
	hostile := "before\x1b[2J\x1b]52;c;QUJD\aafter\x1b]8;;https://evil.example\x1b\\link\x1b]8;;\x1b\\\r\b\x00\u009b31m\u202e"
	clean := Clean(hostile)
	for _, control := range []string{"\x1b", "\a", "\r", "\b", "\x00", "\u009b", "\u202e", "QUJD", "https://evil.example"} {
		if strings.Contains(clean, control) {
			t.Fatalf("control %q survived: %q", control, clean)
		}
	}
	doc := Transcript([]session.Item{{ID: "x", Role: "user", Text: hostile}, {ID: "t", Role: "tool", ToolName: hostile, Text: hostile}}, testTheme(), Options{Width: 60, ExpandTools: true})
	for _, control := range []string{"\x1b[2J", "\x1b]52", "\x1b]8", "\a", "\r", "\b"} {
		if strings.Contains(doc.Text, control) {
			t.Fatalf("injection survived %q", control)
		}
	}
}
func TestSidebarSectionsDimensionsAndRows(t *testing.T) {
	percent := 42.5
	state := session.State{Name: "Main", Thinking: "high", Busy: true, Model: session.ModelInfo{Provider: "native", ID: "model-a"}, Stats: session.Stats{Input: 12000, Output: 1800, CacheRead: 3000, Cost: 0.1234, HasCost: true, ContextPercent: &percent}}
	sessions := []SessionRow{{ID: "a", Name: "Main", Status: "running", Active: true}, {ID: "b", Name: "Background", Status: "waiting"}}
	s := Sidebar(state, sessions, testTheme(), 38, 20)
	plain := ansi.Strip(s)
	rows := strings.Split(plain, "\n")
	if len(rows) != 20 {
		t.Fatalf("height %d", len(rows))
	}
	for _, r := range rows {
		if ansi.StringWidth(r) != 38 {
			t.Fatalf("width %d", ansi.StringWidth(r))
		}
	}
	for _, term := range []string{"SESSION", "STATS", "Main", "$0.12", "42.5%", "in 12K", "out 1.8K", "cache 20.0%", "cost $0.12"} {
		if !strings.Contains(plain, term) {
			t.Errorf("missing %s", term)
		}
	}
	if !strings.Contains(rows[SidebarNewRow], "+ New") || !strings.Contains(rows[SidebarSessionStart+1], "Main") || !strings.Contains(rows[SidebarSessionStart+SidebarSessionRows+1], "Background") {
		t.Fatalf("incorrect click row positions: %q", rows)
	}
	for _, size := range [][2]int{{8, 4}, {16, 15}, {24, 30}, {1, 1}} {
		out := Sidebar(state, sessions, testTheme(), size[0], size[1])
		checkWidth(t, out, size[0])
		if len(strings.Split(out, "\n")) != size[1] {
			t.Fatal("wrong clipped height")
		}
	}
	state.Stats.HasCost = false
	if !strings.Contains(ansi.Strip(Sidebar(state, nil, testTheme(), 38, 20)), "cost —") {
		t.Fatal("unknown cost must be shown as unknown")
	}
	if Sidebar(state, nil, testTheme(), 0, 5) != "" || Transcript(nil, testTheme(), Options{Width: 0}).Text != "" {
		t.Fatal("zero size not empty")
	}
}
func TestCompleteHistoryAndCompactionBoundary(t *testing.T) {
	var items []session.Item
	for i := 0; i < 120; i++ {
		items = append(items, session.Item{ID: string(rune(i + 100)), Role: "user", Text: "Original entry"})
	}
	items = append(items, session.Item{ID: "compact", Role: "compaction", Text: "Context compacted; full history retained"}, session.Item{ID: "last", Role: "assistant", Text: "LATEST_MESSAGE"})
	d := Transcript(items, testTheme(), Options{Width: 60})
	if len(d.Blocks) != 122 || strings.Count(ansi.Strip(d.Text), "Original entry") != 120 || !strings.Contains(ansi.Strip(d.Text), "LATEST_MESSAGE") {
		t.Fatalf("history clipped: blocks=%d originals=%d", len(d.Blocks), strings.Count(ansi.Strip(d.Text), "Original entry"))
	}
}
