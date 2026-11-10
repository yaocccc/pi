package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestToolHeadersUseFoldingPaletteAndTokenThresholds(t *testing.T) {
	theme := testTheme()
	theme.Dim = "#666666"
	item := session.Item{ID: "t", Role: "tool", ToolName: "read", HasResult: true, Arguments: json.RawMessage(`{"path":"sample.ts","offset":7}`), Text: "export const answer = 42;", EstimatedTokens: 5000}
	row := toolCallHeader(item, false, theme, 100)
	if ansi.StringWidth(row) >= 100 || strings.Contains(ansi.Strip(row), "  ≈") {
		t.Fatal("short tool header padded to full width")
	}
	for _, fragment := range []string{paint("✓", theme.Success), paint("read", theme.Text), paint(toolPreview(item), theme.Dim), paint("≈5.0k tok", "#ffa500")} {
		if !strings.Contains(row, fragment) {
			t.Fatalf("missing role color %q in %q", fragment, row)
		}
	}
	if strings.Contains(ansi.Strip(row), `"path":`) {
		t.Fatal("protocol parameters leaked into header")
	}
	for _, tc := range []struct {
		n            int
		label, color string
	}{{4999, "≈5.0k tok", theme.Dim}, {5000, "≈5.0k tok", "#ffa500"}, {9999, "≈10.0k tok", "#ffa500"}, {10000, "≈10.0k tok", theme.Error}, {1_000_000, "≈1.0M tok", theme.Error}} {
		if got := tokenBadge(tc.n, theme); got != paint(tc.label, tc.color) {
			t.Fatalf("token threshold %d: %q", tc.n, got)
		}
	}
	failed := item
	failed.Error = true
	failed.EstimatedTokens = 7000
	group := toolGroupHeader([]session.Item{item, failed}, true, theme, 100)
	for _, fragment := range []string{"2 calls", "2read", "1❌", "≈12.0k", paint(" 1❌", theme.Error)} {
		if !strings.Contains(group, fragment) {
			t.Fatalf("missing summary %q: %s", fragment, group)
		}
	}
	long := item
	long.Arguments = json.RawMessage(`{"path":"` + strings.Repeat("long-path/", 30) + `"}`)
	if got := toolCallHeader(long, false, theme, 200); ansi.StringWidth(got) > 80 {
		t.Fatal("tool summary exceeds 80 columns")
	}
	for width := 1; width <= 132; width++ {
		checkWidth(t, toolCallHeader(item, true, theme, width), width)
		checkWidth(t, toolGroupHeader([]session.Item{item, failed}, false, theme, width), width)
	}
}
func TestCompactToolGroupSummary(t *testing.T) {
	items := []session.Item{
		{ToolName: "edit", HasResult: true, EstimatedTokens: 5000},
		{ToolName: "edit", HasResult: true, EstimatedTokens: 5000},
		{ToolName: "bash", HasResult: true, Error: true, EstimatedTokens: 1200},
	}
	got := ansi.Strip(toolGroupHeader(items, false, testTheme(), 120))
	if got != "› 3 calls · 2edit  1bash 1❌ ≈11.2k" {
		t.Fatal(got)
	}
	items[0].Running = true
	items[1].HasResult = false
	got = ansi.Strip(toolGroupHeader(items, false, testTheme(), 120))
	if strings.Contains(got, "✓") || !strings.Contains(got, "1❌") {
		t.Fatal("unexpected outcome counts: " + got)
	}
	items[2].Error = false
	got = ansi.Strip(toolGroupHeader(items, false, testTheme(), 120))
	if got != "› 3 calls · 2edit  1bash ≈11.2k" {
		t.Fatal(got)
	}
}

func TestToolGroupSpacingAroundProse(t *testing.T) {
	items := []session.Item{{ID: "before", Role: "assistant", Text: "Before"}, {ID: "a", Role: "tool", ToolName: "read", Text: "source"}, {ID: "b", Role: "tool", ToolName: "bash", Text: "result"}, {ID: "reply", Role: "assistant", Text: "Done"}}
	for _, expanded := range []bool{false, true} {
		for _, results := range []bool{false, true} {
			opts := Options{Width: 80, Expanded: map[string]bool{"tool-group:a": expanded, "a": results, "b": results}}
			doc := Transcript(items, testTheme(), opts)
			group := blockByID(t, doc, "tool-group:a")
			before := blockByID(t, doc, "before")
			reply := blockByID(t, doc, "reply")
			rows := strings.Split(ansi.Strip(doc.Text), "\n")
			if group.End != group.Start+1 || group.Start != before.End+1 || strings.Trim(rows[before.End], " │") != "" {
				t.Fatal("prose → group must have one blank row, with a one-row header hit range")
			}
			end := group.End
			if expanded {
				first := blockByID(t, doc, "a")
				last := blockByID(t, doc, "b")
				if first.Start != group.End || strings.Trim(rows[group.End], " │") == "" {
					t.Fatal("group header → first child must be adjacent")
				}
				end = last.End
			}
			if reply.Start != end+1 || strings.Trim(rows[end], " │") != "" {
				t.Fatal("whole group → prose must have exactly one blank row")
			}
			tail := Transcript(items[:3], testTheme(), opts)
			tailRows := strings.Split(ansi.Strip(tail.Text), "\n")
			if strings.Trim(tailRows[len(tailRows)-2], " │") == "" {
				t.Fatal("group without following prose must not leave a blank row")
			}
		}
	}
}

func TestSidebarSharedFrameAndTwoLineCards(t *testing.T) {
	sessions := []SessionRow{{ID: "1", Name: "First", Status: "idle", Active: true}, {ID: "2", Name: "Second", Status: "input +1"}}
	for _, width := range []int{28, 32, 38} {
		rows := strings.Split(ansi.Strip(Sidebar(session.State{}, sessions, testTheme(), width, 24)), "\n")
		for _, i := range []int{0, 6, 10, 23} {
			left, right := "│ ", " │"
			if i == 0 {
				left, right = "┌", "┐"
			}
			if i == 23 {
				left, right = "└", "┘"
			}
			if !strings.HasPrefix(rows[i], left) || !strings.HasSuffix(rows[i], right) {
				t.Fatalf("frame row %d: %q", i, rows[i])
			}
		}
		if !strings.Contains(rows[SidebarSessionStart], "1 idle") || !strings.Contains(rows[SidebarSessionStart+1], "First") || !strings.Contains(rows[SidebarSessionStart+2], "2 等待回答 +1") || !strings.Contains(rows[SidebarSessionStart+3], "Second") {
			t.Fatalf("two-line cards lost: %q", rows)
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatalf("width=%d row=%q", width, row)
			}
		}
	}
}
