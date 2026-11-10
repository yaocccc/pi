package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestSidebarInformationGeometryAndAlignment(t *testing.T) {
	tokens, percent := 124200, 12.4
	state := session.State{Name: "Current session", Cwd: "/work/project", Thinking: "high", Model: session.ModelInfo{ID: "gpt-6-astra", ContextWindow: 1000000}, Stats: session.Stats{Input: 13200000, Output: 123000, Total: 99999999, CacheRead: 96800000, Cost: 12.2, HasCost: true, ContextTokens: &tokens, ContextPercent: &percent}}
	for _, git := range []*GitStatus{nil, {Branch: "main", Modified: 2, Staged: 1, Untracked: 3}} {
		ctx := SidebarContext{Directory: state.Cwd, Git: git}
		layout := SidebarGeometry(36, ctx)
		raw := Sidebar(state, []SessionRow{{ID: "1", Name: "Selected", Status: "idle", Active: true}}, testTheme(), 36, 36, ctx)
		rows := strings.Split(ansi.Strip(raw), "\n")
		if !strings.Contains(rows[2], state.Name) || !strings.Contains(rows[3], state.Cwd) {
			t.Fatal("missing name or directory")
		}
		if strings.Contains(ansi.Strip(raw), "main") || layout.ContextRow != 4 {
			t.Fatal("git information must not affect the sidebar")
		}
		if !strings.Contains(rows[layout.ContextRow], "CTX ") || strings.ContainsAny(rows[layout.ContextRow], "[]") || !strings.Contains(rows[layout.ContextRow], "12.4%") || !strings.Contains(rows[layout.ContextRow], "124.2K/1M") {
			t.Fatal("missing two-line context display")
		}
		if !strings.Contains(rows[layout.StatsRow+1], "in 13.2M") || !strings.Contains(rows[layout.StatsRow+1], "13.2M") || !strings.Contains(rows[layout.StatsRow+1], "out 123K") || !strings.Contains(rows[layout.StatsRow+1], "123K") {
			t.Fatal("missing compact token stats")
		}
		if !strings.Contains(rows[layout.StatsRow+2], "cache 88.0%") || !strings.Contains(rows[layout.StatsRow+2], "88.0%") || !strings.Contains(rows[layout.StatsRow+2], "$12.20") {
			t.Fatalf("missing second stats row: %q", rows[layout.StatsRow+2])
		}
		plain := strings.Join(rows, "\n")
		for _, absent := range []string{"gpt-6-astra", "high", "TOTAL", "SESSIONS", "Weekly limit"} {
			if strings.Contains(plain, absent) {
				t.Fatalf("unexpected %s", absent)
			}
		}
		if !strings.Contains(rows[layout.NewRow], "+ New session") || !strings.Contains(rows[layout.SessionsStart+1], "Selected") {
			t.Fatal("optional git row shifted actions incorrectly")
		}
		for i, row := range strings.Split(raw, "\n") {
			hasBG := strings.Contains(row, "\x1b[48;2;82;85;88m")
			if hasBG != (i == layout.SessionsStart || i == layout.SessionsStart+1) {
				t.Fatalf("selection background at wrong row %d", i)
			}
		}
	}
	state.Stats.ContextTokens = nil
	state.Stats.ContextPercent = nil
	if got := ansi.Strip(progressRow(state, testTheme(), 28)); !strings.Contains(got, "—%") || strings.Contains(got, "0.0%") {
		t.Fatalf("unknown context presented as zero: %q", got)
	}
	if number(1300000) != "1.3M" || number(123000) != "123K" || number(2400000) != "2.4M" {
		t.Fatal("incorrect compact numbers")
	}
}
func TestSidebarLongDirectoryAndModelAreTruncated(t *testing.T) {
	state := session.State{Cwd: "/very/long/" + strings.Repeat("directory/", 20), Model: session.ModelInfo{ID: strings.Repeat("long-model-", 10)}, Thinking: "xhigh"}
	rows := strings.Split(ansi.Strip(Sidebar(state, nil, testTheme(), 32, 25)), "\n")
	if !strings.Contains(rows[3], "…") || strings.Contains(strings.Join(rows, "\n"), "long-model-") {
		t.Fatalf("directory/model handling: %q", rows[:5])
	}
	for _, width := range []int{1, 4, 8, 16, 28, 32, 48} {
		checkWidth(t, Sidebar(state, nil, testTheme(), width, 25), width)
	}
}
