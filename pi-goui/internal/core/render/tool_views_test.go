package render

import (
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/session"
)

func TestTranscriptProviderIdentityInvalidatesCache(t *testing.T) {
	makeProvider := func(marker string) *ToolViews {
		return &ToolViews{
			Layout: func(_ extensionapi.CardHost, item session.Item, _ config.Theme, _ extensionapi.CardOptions, _ int) ([]string, []extensionapi.Block, bool) {
				return []string{marker}, []extensionapi.Block{{ID: item.ID, Role: item.Role, Start: 0, End: 1}}, true
			}}
	}
	items := []session.Item{{ID: "tool", Role: "tool", ToolName: "sample", HasResult: true, Text: "GENERIC_RESULT"}}
	opts := Options{Width: 80, ExpandTools: true, Tools: makeProvider("PROVIDER_ONE")}
	var cache TranscriptCache
	first := cache.Transcript(items, testTheme(), opts)
	if !strings.Contains(first.Text, "PROVIDER_ONE") {
		t.Fatal(first.Text)
	}
	opts.Tools = makeProvider("PROVIDER_TWO")
	second := cache.Transcript(items, testTheme(), opts)
	if strings.Contains(second.Text, "PROVIDER_ONE") || !strings.Contains(second.Text, "PROVIDER_TWO") {
		t.Fatal(second.Text)
	}
	opts.Tools = nil
	fallback := cache.Transcript(items, testTheme(), opts)
	if strings.Contains(fallback.Text, "PROVIDER_") || !strings.Contains(fallback.Text, "GENERIC_RESULT") {
		t.Fatal(fallback.Text)
	}
}

func TestEmptyToolProviderPreservesGenericFallback(t *testing.T) {
	item := session.Item{ID: "tool", Role: "tool", ToolName: "sample", HasResult: true, Text: "GENERIC_RESULT"}
	opts := Options{Width: 80, ExpandTools: true}
	plain := Transcript([]session.Item{item}, testTheme(), opts)
	opts.Tools = &ToolViews{}
	if got := Transcript([]session.Item{item}, testTheme(), opts); got.Text != plain.Text {
		t.Fatalf("empty provider changed transcript: %s", got.Text)
	}
	if got, want := strings.Join(ToolResultRows(item, testTheme(), 80, &ToolViews{}), "\n"), strings.Join(ToolResultRows(item, testTheme(), 80), "\n"); got != want {
		t.Fatalf("empty provider changed detail: %s", got)
	}
}
