package render

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func mockQuota() *SidebarQuota {
	remaining, credits := 80.2, 82334.0
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return &SidebarQuota{RemainingPercent: &remaining, ResetAt: now.Add(49 * time.Hour), Credits: &credits, Now: now}
}
func TestMockSidebarOptionalSectionsAndHitGeometry(t *testing.T) {
	for _, withGit := range []bool{false, true} {
		for _, withQuota := range []bool{false, true} {
			ctx := SidebarContext{Directory: "/project"}
			if withGit {
				ctx.Git = &GitStatus{Branch: "main"}
			}
			if withQuota {
				ctx.Quota = mockQuota()
			}
			layout := SidebarGeometry(36, ctx)
			rows := strings.Split(Sidebar(session.State{Name: "Session A"}, []SessionRow{{ID: "1", Name: "", Status: "idle", Active: true}}, testTheme(), 36, 36, ctx), "\n")
			plain := make([]string, len(rows))
			for i, row := range rows {
				plain[i] = ansi.Strip(row)
				if ansi.StringWidth(row) != 36 {
					t.Fatalf("row %d wrong width: %q", i, row)
				}
			}
			if !strings.HasPrefix(plain[0], "┌") || !strings.HasSuffix(plain[0], "┐") || !strings.HasPrefix(plain[35], "└") || !strings.HasSuffix(plain[35], "┘") {
				t.Fatal("incorrect outer frame")
			}
			if !strings.Contains(plain[1], "SESSION") || !strings.Contains(plain[layout.StatsRow], "STATS") {
				t.Fatal("headings must have own row")
			}
			for _, index := range []int{1, layout.StatsRow} {
				if !strings.Contains(rows[index], "\x1b[1;") && !strings.Contains(rows[index], "\x1b[1m") {
					t.Fatalf("heading not bold: %q", rows[index])
				}
			}
			if !strings.HasPrefix(plain[layout.StatsRow-1], "│ ─") || !strings.HasPrefix(plain[layout.StatsRow+3], "│ ─") {
				t.Fatal("missing inset dividers")
			}
			if !strings.Contains(plain[layout.NewRow], "+ New session") || !strings.Contains(plain[layout.SessionsStart], "1 idle") {
				t.Fatal("geometry does not match actions")
			}
			if strings.TrimSpace(strings.Trim(plain[layout.SessionsStart+1], "│")) != "" {
				t.Fatal("blank session name was invented")
			}
			all := strings.Join(plain, "\n")
			if withQuota {
				if layout.QuotaRow < 0 {
					t.Fatal("missing quota geometry")
				}
				for _, term := range []string{"Weekly limit", "80.2%", "Resets in", "2d 1h", "Credits", "82334"} {
					if !strings.Contains(all, term) {
						t.Fatalf("missing quota field %s", term)
					}
				}
				if !strings.HasPrefix(plain[layout.NewRow-1], "│ ─") {
					t.Fatal("quota requires ending divider")
				}
			} else if layout.QuotaRow != -1 || strings.Contains(all, "Weekly limit") || strings.Contains(all, "Credits") {
				t.Fatal("quota appeared without explicit context")
			}
			if strings.Contains(all, "SESSIONS") || strings.Contains(all, "TOTAL") {
				t.Fatal("removed heading/row remains")
			}
			if layout.Capacity != (36-layout.SessionsStart-1)/2 {
				t.Fatal("incorrect complete card capacity")
			}
		}
	}
}
func TestMockSidebarQuotaUnknownAndFormatting(t *testing.T) {
	q := mockQuota()
	for _, tc := range []struct {
		duration time.Duration
		want     string
	}{{49 * time.Hour, "2d 1h"}, {61 * time.Minute, "1h 1m"}, {time.Second, "1m"}, {-time.Second, "0m"}} {
		q.ResetAt = q.Now.Add(tc.duration)
		if got := quotaCountdown(q); got != tc.want {
			t.Fatalf("countdown %v: %s", tc.duration, got)
		}
	}
	q.Now = time.Time{}
	if quotaCountdown(q) != "—" {
		t.Fatal("render must not use global clock")
	}
	q.Credits = nil
	if quotaCredits(q) != "—" {
		t.Fatal("unknown credits")
	}
	q.UnlimitedCredits = true
	if quotaCredits(q) != "unlimited" {
		t.Fatal("unlimited credits")
	}
	q.UnlimitedCredits = false
	v := 82334.25
	q.Credits = &v
	if quotaCredits(q) != "82334" {
		t.Fatal("credits must be displayed as an integer")
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), -1} {
		q.Credits = &bad
		if quotaCredits(q) != "—" {
			t.Fatal("invalid credits shown")
		}
	}
	if quotaPercent(nil) != "—" {
		t.Fatal("unknown percentage")
	}
	bad := math.NaN()
	if quotaPercent(&bad) != "—" {
		t.Fatal("NaN percentage")
	}
	low, high := -10.0, 110.0
	if quotaPercent(&low) != "0.0%" || quotaPercent(&high) != "100.0%" {
		t.Fatal("quota percentage not bounded")
	}
}
func TestMockSidebarWidthsClippingAndStatsColumns(t *testing.T) {
	q := mockQuota()
	ctx := SidebarContext{Directory: "目录/" + strings.Repeat("x", 200), Git: &GitStatus{Branch: "分支/" + strings.Repeat("y", 200)}, Quota: q}
	state := session.State{Name: "长会话 👩‍💻", Stats: session.Stats{Input: 13200000, Output: 123000, CacheRead: 100000, HasCost: true, Cost: 12.2}}
	sessions := []SessionRow{{ID: "1", Name: "first", Active: true}, {ID: "2", Name: "second"}}
	for _, width := range []int{1, 2, 3, 4, 8, 16, 28, 36, 80} {
		for height := 1; height <= 36; height++ {
			text := Sidebar(state, sessions, testTheme(), width, height, ctx)
			lines := strings.Split(text, "\n")
			if len(lines) != height {
				t.Fatalf("height %d got %d", height, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != width {
					t.Fatalf("size %dx%d bad row %q", width, height, line)
				}
			}
			layout := SidebarGeometry(height, ctx)
			if layout.Capacity == 0 && strings.Contains(ansi.Strip(text), "first") {
				t.Fatal("partial session card was rendered")
			}
		}
	}
	ctx = SidebarContext{}
	rows := strings.Split(ansi.Strip(Sidebar(state, nil, testTheme(), 36, 30, ctx)), "\n")
	layout := SidebarGeometry(30, ctx)
	first, second := rows[layout.StatsRow+1], rows[layout.StatsRow+2]
	if strings.Index(first, "in ") != strings.Index(second, "cache ") || strings.Index(first, "out ") != strings.Index(second, "cost ") {
		t.Fatal("stats labels are not aligned to columns")
	}
	if !strings.Contains(first, "in 13.2M") || !strings.Contains(first, "out 123K") || !strings.Contains(second, "cost $12.20") {
		t.Fatal("stats labels must be adjacent to their values")
	}
}
