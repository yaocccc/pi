package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestCompactionStats(t *testing.T) {
	for _, tc := range []struct {
		before, after int
		want          string
	}{
		{80000, 24000, "≈80K → 24K tokens · −70%"},
		{100, 125, "≈100 → 125 tokens · +25%"},
		{100, 100, "≈100 → 100 tokens · 0%"},
		{100, 0, "≈100 → 0 tokens · −100%"},
		{0, 10, "≈0 → 10 tokens"},
		{0, 0, "≈0 → 0 tokens"},
		{-1, 10, ""},
	} {
		got := compactionStats(&session.CompactionMetrics{BeforeTokens: tc.before, AfterTokens: tc.after})
		if got != tc.want {
			t.Errorf("%d → %d: got %q, want %q", tc.before, tc.after, got, tc.want)
		}
	}
	if compactionStats(nil) != "" {
		t.Fatal("invented missing metrics")
	}
}

func TestCompactionStatsInvalidateTranscriptCache(t *testing.T) {
	var cache TranscriptCache
	items := []session.Item{{ID: "c", Role: "system", Text: "Context compacted — earlier messages retained\nSummary"}}
	opts := Options{Width: 80}
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	items[0].Compaction = &session.CompactionMetrics{BeforeTokens: 80000, AfterTokens: 24000}
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	items[0].Compaction.AfterTokens = 40000
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
	items[0].Compaction = nil
	assertCachedTranscript(t, &cache, items, testTheme(), opts)
}

func TestCompactionStatsTranscript(t *testing.T) {
	item := session.Item{ID: "c", Role: "system", Text: "Context compacted — earlier messages retained\n# Summary\nContinue verification",
		Compaction: &session.CompactionMetrics{BeforeTokens: 80000, AfterTokens: 24000}}
	for _, width := range []int{80, 32, 20, 7} {
		doc := Transcript([]session.Item{item}, testTheme(), Options{Width: width})
		plain := ansi.Strip(doc.Text)
		for _, token := range []string{"≈80K", "24K", "tokens", "−70%"} {
			if !strings.Contains(plain, token) {
				t.Errorf("width %d lost %q:\n%s", width, token, plain)
			}
		}
		if strings.Contains(plain, "×") {
			t.Fatal("multiplicative ratio should not be shown")
		}
		for _, line := range strings.Split(doc.Text, "\n") {
			if ansi.StringWidth(line) > width {
				t.Errorf("width %d overflow: %q", width, line)
			}
		}
		if len(doc.Blocks) != 1 || doc.Blocks[0].End != len(strings.Split(doc.Text, "\n")) {
			t.Fatalf("metrics escaped their checkpoint block: %#v", doc.Blocks)
		}
	}
	item.Compaction = nil
	plain := ansi.Strip(Transcript([]session.Item{item}, testTheme(), Options{Width: 80}).Text)
	if !strings.Contains(plain, "Context compacted · Continue verification") || strings.Contains(plain, "tokens") {
		t.Fatal("old checkpoint rendering changed:", plain)
	}
}
