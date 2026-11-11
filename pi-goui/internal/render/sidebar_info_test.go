package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestSidebarModeGeometry(t *testing.T) {
	for _, mode := range []string{"normal", "fast", "ultrafast"} {
		ctx := SidebarContext{Mode: mode, Quota: &SidebarQuota{}, History: []string{"recent"}}
		layout := SidebarGeometry(40, ctx)
		rows := strings.Split(ansi.Strip(Sidebar(session.State{}, nil, testTheme(), 36, 40, ctx)), "\n")
		if layout.ModeRow != layout.QuotaRow+3 || !strings.Contains(rows[layout.ModeRow], "Mode") || !strings.Contains(rows[layout.ModeRow], CodexModeLabel(mode)) {
			t.Fatal("Mode row not aligned")
		}
		if layout.QuotaRow != 10 || !strings.Contains(rows[layout.QuotaRow], "Weekly limit") {
			t.Fatal("Mode must not shift quota geometry")
		}
		if !strings.Contains(rows[layout.HistoryRow], "RECENTS") || !strings.Contains(rows[layout.NewRow], "+ New") {
			t.Fatal("Mode shifted history/action geometry incorrectly")
		}
	}
}

func TestSidebarModeBelowCredits(t *testing.T) {
	ctx := SidebarContext{Mode: "ultrafast", Quota: &SidebarQuota{}, History: []string{"first", "second", "third", "fourth", "fifth"}}
	sessions := make([]SessionRow, 30)
	for i := range sessions {
		sessions[i] = SessionRow{ID: "1", Name: "Open session", Active: true}
	}
	for _, height := range []int{1, 2, 3, 8, 16, 24, 40, 60} {
		for _, width := range []int{8, 16, 24, 36} {
			layout := SidebarGeometry(height, ctx)
			raw := Sidebar(session.State{}, sessions, testTheme(), width, height, ctx)
			rows := strings.Split(ansi.Strip(raw), "\n")
			if len(rows) != height {
				t.Fatalf("height %d: got %d rows", height, len(rows))
			}
			checkWidth(t, raw, width)
			if layout.ModeRow >= 0 && layout.ModeRow < height-1 {
				if layout.ModeRow != layout.QuotaRow+3 || (width >= 9 && !strings.Contains(rows[layout.ModeRow], "Mode")) {
					t.Fatalf("missing Mode below Credits at %dx%d", width, height)
				}
				if layout.SessionsStart <= layout.ModeRow {
					t.Fatal("session cards overlap Mode")
				}
				value, offset := SidebarModeValue(ctx.Mode, width-4)
				if value != "" && ansi.Cut(rows[layout.ModeRow], 2+offset, 2+offset+ansi.StringWidth(value)) != value {
					t.Fatal("mode value geometry differs from rendered row")
				}
			}
		}
	}
}

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
		if !strings.Contains(rows[layout.NewRow], "+ New") || !strings.Contains(rows[layout.SessionsStart+1], "Selected") {
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
