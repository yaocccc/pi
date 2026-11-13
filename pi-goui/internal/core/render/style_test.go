package render

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestTransparentBackgrounds(t *testing.T) {
	items := []session.Item{
		{ID: "u", Role: "user", Text: "# Heading\n\nParagraph with `inline code`, **bold**, ~~strike~~ and [link](https://example.com).\n\n> quoted\n\n---\n\n```go\nfunc main() { println(42) }\n```"},
		{ID: "a", Role: "assistant", Text: "# Answer\n\n```text\ncode output\n```\n\n| Table | Value |\n|---|---|\n| a | b |", Thinking: "thinking"},
		{ID: "t", Role: "tool", ToolName: "bash", Text: "output", Arguments: json.RawMessage(`{"command":"pwd"}`)},
	}
	doc := Transcript(items, testTheme(), Options{Width: 80, ExpandTools: true, ShowThinking: true})
	// 49/default background reset is allowed; actual color fills are not.
	sgr := regexp.MustCompile("\x1b\\[([0-9;:]*)m")
	if len(sgr.FindAllStringSubmatch(doc.Text, -1)) == 0 {
		t.Fatal("transparency assertion requires ANSI-colored Markdown output")
	}
	for _, rendered := range []string{doc.Text, Sidebar(session.State{}, nil, testTheme(), 28, 20)} {
		for _, match := range sgr.FindAllStringSubmatch(rendered, -1) {
			params := strings.FieldsFunc(match[1], func(r rune) bool { return r == ';' || r == ':' })
			for i := 0; i < len(params); i++ {
				n, _ := strconv.Atoi(params[i])
				if n == 38 { // Skip RGB/index parameters, which may numerically equal an SGR code.
					if i+1 < len(params) && params[i+1] == "2" {
						i += 4
					} else if i+1 < len(params) && params[i+1] == "5" {
						i += 2
					}
					continue
				}
				if n == 48 || (n >= 40 && n <= 47) || (n >= 100 && n <= 107) {
					t.Fatalf("explicit background: %q", match[0])
				}
			}
		}
	}
}
func toolFixture() []session.Item {
	return []session.Item{{ID: "t1", Role: "tool", ToolName: "read", Text: "first preview\nFIRST_OUTPUT"}, {ID: "t2", Role: "toolResult", ToolName: "read", Text: "second preview\nSECOND_OUTPUT"}, {ID: "t3", Role: "tool", ToolName: "bash", Text: "third preview\nTHIRD_OUTPUT"}}
}
func blockByID(t *testing.T, d Document, id string) Block {
	t.Helper()
	for _, b := range d.Blocks {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing block %s: %+v", id, d.Blocks)
	return Block{}
}
func TestConsecutiveToolGroupsAndEffectiveState(t *testing.T) {
	items := toolFixture()
	id := "tool-group:t1"
	closed := Transcript(items, testTheme(), Options{Width: 80})
	if len(closed.Blocks) != 1 || !strings.Contains(ansi.Strip(closed.Text), "3 calls · 2read  1bash") {
		t.Fatalf("not grouped: %+v %s", closed.Blocks, closed.Text)
	}
	b := blockByID(t, closed, id)
	if b.Role != "toolGroup" || !b.Foldable || b.Expanded || b.End-b.Start != 1 {
		t.Fatalf("header contract: %+v", b)
	}
	opened := Transcript(items, testTheme(), Options{Width: 80, Expanded: map[string]bool{id: true, "t2": true}})
	if len(opened.Blocks) != 4 || !strings.Contains(opened.Text, "SECOND_OUTPUT") || strings.Contains(opened.Text, "FIRST_OUTPUT") {
		t.Fatalf("independent children: %+v %s", opened.Blocks, opened.Text)
	}
	if !blockByID(t, opened, id).Expanded || !blockByID(t, opened, "t2").Expanded || blockByID(t, opened, "t1").Expanded {
		t.Fatal("incorrect effective expansion")
	}
	rows := strings.Split(ansi.Strip(opened.Text), "\n")
	for i, b := range opened.Blocks {
		if b.Start < 0 || b.End > len(rows) || b.End <= b.Start {
			t.Fatalf("bad offset: %+v", b)
		}
		if i > 0 && opened.Blocks[i-1].End > b.Start {
			t.Fatal("overlapping hit targets")
		}
	}
	global := Transcript(items, testTheme(), Options{Width: 80, ExpandTools: true, Expanded: map[string]bool{"t2": false}})
	if !strings.Contains(global.Text, "FIRST_OUTPUT") || strings.Contains(global.Text, "SECOND_OUTPUT") || !strings.Contains(global.Text, "THIRD_OUTPUT") {
		t.Fatal("global/child overrides")
	}
	hidden := Transcript(items, testTheme(), Options{Width: 80, ExpandTools: true, Expanded: map[string]bool{id: false}})
	if len(hidden.Blocks) != 1 || blockByID(t, hidden, id).Expanded || strings.Contains(hidden.Text, "OUTPUT") {
		t.Fatal("group false must override global")
	}
	more := append(items, session.Item{ID: "t4", Role: "tool", ToolName: "bash", Running: true})
	streaming := Transcript(more, testTheme(), Options{Width: 80, Expanded: map[string]bool{id: true}})
	if !blockByID(t, streaming, id).Expanded || !strings.Contains(ansi.Strip(streaming.Text), "4 calls") || strings.Contains(ansi.Strip(streaming.Text), "1 running") {
		t.Fatal("appended tool lost group state")
	}
	more[1].Error = true
	failed := Transcript(more, testTheme(), Options{Width: 80})
	if !strings.Contains(ansi.Strip(failed.Text), "4 calls") || strings.Contains(ansi.Strip(failed.Text), "1 running") || !strings.Contains(ansi.Strip(failed.Text), "1❌") {
		t.Fatal("group must hide running count and retain error summary")
	}
}
func TestGroupsRespectVisibleBoundaries(t *testing.T) {
	for _, sep := range []session.Item{{ID: "a", Role: "assistant", Text: "prose"}, {ID: "u", Role: "user", Text: "prompt"}, {ID: "s", Role: "system", Text: "summary"}, {ID: "th", Role: "thinking", Text: "thought"}} {
		items := []session.Item{toolFixture()[0], sep, toolFixture()[1]}
		d := Transcript(items, testTheme(), Options{Width: 60, ShowThinking: true})
		groups := 0
		for _, b := range d.Blocks {
			if b.Role == "toolGroup" {
				groups++
			}
		}
		if groups != 2 || strings.Count(ansi.Strip(d.Text), "1 calls") != 2 {
			t.Fatalf("group crossed %s: %+v", sep.Role, d.Blocks)
		}
	}
	for _, sep := range []session.Item{{ID: "th", Role: "thinking", Text: "hidden"}, {ID: "a", Role: "assistant", Thinking: "hidden"}} {
		items := []session.Item{toolFixture()[0], sep, toolFixture()[1]}
		d := Transcript(items, testTheme(), Options{Width: 60})
		if len(d.Blocks) != 1 || d.Blocks[0].Role != "toolGroup" {
			t.Fatalf("hidden thinking split group: %+v", d.Blocks)
		}
	}
}
func TestSummaryOneContentRowPreservesSource(t *testing.T) {
	original := session.Item{ID: "c", Role: "system", Text: "Context compacted — earlier messages retained\n# Summary\nKeep the current design.\n" + strings.Repeat("VERY_LONG_FULL_SUMMARY\n", 100)}
	for _, width := range []int{1, 8, 16, 40, 80, 132} {
		items := []session.Item{original}
		d := Transcript(items, testTheme(), Options{Width: width, ExpandTools: true})
		wantRows := 1
		if width >= 8 {
			wantRows = 3
		}
		if len(strings.Split(d.Text, "\n")) != wantRows || len(d.Blocks) != 1 || d.Blocks[0].End-d.Blocks[0].Start != wantRows {
			t.Fatalf("summary not one framed content row: %q %+v", d.Text, d.Blocks)
		}
		if width >= 12 && !strings.HasPrefix(ansi.Strip(d.Text), "╭─ 系统 ") {
			t.Fatal("missing left-aligned System title")
		}
		checkWidth(t, d.Text, width)
		if !reflect.DeepEqual(items[0], original) {
			t.Fatal("summary source mutated")
		}
		if width >= 80 && !strings.Contains(ansi.Strip(d.Text), "Context compacted · Keep the current design.") {
			t.Fatal("missing synopsis")
		}
	}
	for _, width := range []int{8, 16, 40, 80} {
		d := Transcript(toolFixture(), testTheme(), Options{Width: width, ExpandTools: true})
		checkWidth(t, d.Text, width)
	}
}
