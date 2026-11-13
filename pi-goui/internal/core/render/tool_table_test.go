package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestResultTablesAndNarrowFallback(t *testing.T) {
	for _, name := range []string{"unknown", "mcp.inventory.list"} {
		item := visualItem(name, nil, nil, `[{"name":"東京","count":2},{"name":"Paris","count":3}]`)
		wide := ansi.Strip(visualText(item, 80))
		mustContainVisual(t, wide, "Count", "Name", "東京", "Paris", "─┼─")
		for _, width := range []int{1, 5, 12, 20, 80} {
			rendered := visualText(item, width)
			for _, line := range strings.Split(rendered, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s width %d: %q", name, width, line)
				}
			}
		}
		narrow := ansi.Strip(visualText(item, 12))
		if strings.Contains(narrow, "─┼─") {
			t.Fatalf("table did not fall back: %s", narrow)
		}
		mustContainVisual(t, strings.Join(strings.Fields(narrow), ""), "Paris", "東京")
	}
}

func TestResultTableLosslessFallbacks(t *testing.T) {
	for _, payload := range []string{
		`[{"a":1,"b":"line\nbreak"},{"a":2,"b":"plain"}]`,
		`[{"a":1,"b":{"nested":true}},{"a":2,"b":{}}]`,
		`[{"a":1,"b":2},{"a":3,"c":4}]`,
		`[{"data":"SECRET","mimeType":"image/png"},{"data":"SECRET","mimeType":"image/png"}]`,
	} {
		text := ansi.Strip(visualText(visualItem("mcp.any", nil, nil, payload), 100))
		if strings.Contains(text, "─┼─") || strings.Contains(text, "SECRET") {
			t.Fatalf("unsafe table: %s", text)
		}
	}
	item := visualItem("mcp.any", nil, nil, `[{"name":"\u001b]52;c;SECRET\u0007safe","ok":true},{"name":"normal","ok":false}]`)
	text := visualText(item, 100)
	if strings.Contains(text, "\x1b]52") {
		t.Fatal("untrusted terminal control retained")
	}
	malformed := visualItem("unknown", nil, nil, `{"unfinished":`)
	mustContainVisual(t, visualText(malformed, 80), `{"unfinished":`)
}

func TestUsefulResultSynopsis(t *testing.T) {
	cases := []struct {
		item session.Item
		want string
	}{
		{visualItem("mcp.any", nil, nil, `{"count":12,"items":[1,2]}`), "Count: 12 · Items: 2 items"},
		{visualItem("mcp.any", nil, nil, "first\nsecond\nthird"), "first · 3 lines"},
		{visualItem("mcp.any", nil, nil, ""), "Empty result"},
		{session.Item{ToolName: "unknown", Running: true}, "Waiting for output"},
		{session.Item{ToolName: "unknown", Error: true}, "Failed · no error output"},
		{visualItem("mcp.any", nil, nil, `{"data":"SECRET","mimeType":"image/png"}`), "Image attachment · image/png"},
	}
	for _, tc := range cases {
		preview := toolPreview(tc.item)
		if !strings.Contains(preview, tc.want) || strings.Contains(preview, "SECRET") {
			t.Fatalf("preview %q, want %q", preview, tc.want)
		}
	}
	failed := visualItem("edit", map[string]any{"path": "x.go", "oldText": "a", "newText": "b"}, nil, `{"error":"denied","status":"failed"}`)
	failed.Error = true
	mustContainVisual(t, toolPreview(failed), "denied")
	failed.Text = ""
	mustContainVisual(t, toolPreview(failed), "Failed · no error output")
}
