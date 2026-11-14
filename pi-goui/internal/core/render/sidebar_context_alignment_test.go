package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestContextProgressWholeLines(t *testing.T) {
	for _, tc := range []struct {
		percent float64
		bar     string
	}{
		{0, strings.Repeat("─", 8)},
		{0.01, "━" + strings.Repeat("─", 7)},
		{1, "━" + strings.Repeat("─", 7)},
		{4, "━" + strings.Repeat("─", 7)},
		{12.5, "━" + strings.Repeat("─", 7)},
		{50, "━━━━" + strings.Repeat("─", 4)},
		{100, "━━━━━━━━"},
		{120, "━━━━━━━━"},
	} {
		state := session.State{Stats: session.Stats{ContextPercent: &tc.percent}}
		row := ansi.Strip(progressRow(state, testTheme(), 32))
		if !strings.HasPrefix(row, "ctx "+tc.bar+" ") {
			t.Fatalf("percent %g: unexpected bar %q", tc.percent, row)
		}
	}
}

func TestContextPercentageAlignsWithCostLabel(t *testing.T) {
	for _, width := range []int{24, 31, 32, 40, 60} {
		percent := 12.3
		state := session.State{Stats: session.Stats{ContextPercent: &percent}}
		ctx := ansi.Strip(progressRow(state, testTheme(), width))
		bar := strings.Fields(ctx)[1]
		if ansi.StringWidth(bar) > 8 {
			t.Fatalf("width %d: bar exceeds 8 cells: %q", width, bar)
		}
		stats := ansi.Strip(statsColumns("cache", "20.0%", "cost", "$0.12", width, testTheme()))
		percentColumn := ansi.StringWidth(strings.SplitN(ctx, "12.3%", 2)[0])
		costColumn := strings.Index(stats, "cost")
		if !strings.Contains(ctx, "12.3%") || percentColumn != costColumn {
			t.Fatalf("width %d: percentage column %d != cost column %d: %q / %q", width, percentColumn, costColumn, ctx, stats)
		}
	}
}
