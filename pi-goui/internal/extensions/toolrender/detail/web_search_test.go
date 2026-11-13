package detail_test

import (
	"encoding/json"
	"pi-goui/internal/core/render/tooldetail"
	"pi-goui/internal/extensions/toolrender"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func webTestRows(t *testing.T, name string, item session.Item, width int, wrap bool) []string {
	t.Helper()
	hooks := testHooks(t)
	if wrap {
		hooks.Line = func(s, color string, w int) []string {
			if w != width {
				t.Fatalf("wrong width: %d", w)
			}
			return strings.Split(ansi.Hardwrap(s, w, true), "\n")
		}
	}
	original := decodeMap(item.Details)
	details := copyMap(original)
	before, _ := json.Marshal(original)
	rows := renderAdapter(t, name, item, decodeMap(item.Arguments), details, width, hooks)
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("mutated input")
	}
	return rows
}

func TestWebSearchDetailsSchemaAndCompleteText(t *testing.T) {
	item := session.Item{HasResult: true, Text: "PROVIDER_TEXT\n\n  中文😀 result END \n",
		Arguments: json.RawMessage(`{"queries":["中文搜索","second"]}`),
		Details:   json.RawMessage(`{"queries":["中文搜索","second"],"queryCount":2,"successfulQueries":1,"totalResults":5,"searchId":"search-42","fetchId":null,"queryProviders":[{"query":"中文搜索","providers":["exa"]}],"truncated":true,"originalChars":900719925474099312345,"returnedChars":80,"omittedChars":30,"future":{"keep":"UNKNOWN"}}`)}
	got := strings.Join(webTestRows(t, "web_search", item, 60, false), "\n")
	for _, want := range []string{"Search · returned", "Queries\n• 中文搜索", "Search ID: search-42", "Providers by query", "Upstream output truncated", "900719925474099312345", "UNKNOWN", item.Text} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Count(got, "PROVIDER_TEXT") != 1 || strings.Contains(got, "Sources\n") {
		t.Fatal("invented sources or duplicated text: " + got)
	}
}

func TestWebRenderersDelegateWidthWithoutTruncating(t *testing.T) {
	for _, method := range []string{"web_search", "fetch_content", "source_check", "get_search_content"} {
		item := session.Item{Running: true, Text: strings.Repeat("中文😀", 45) + "TAIL",
			Details: json.RawMessage(`{"unknown":"UNKNOWN_END"}`)}
		for _, width := range []int{1, 2, 5, 20, 40, 60} {
			rows := webTestRows(t, method, item, width, true)
			joined := strings.Join(rows, "")
			if !strings.Contains(joined, item.Text) || !strings.Contains(joined, "UNKNOWN_END") || !strings.Contains(joined, "running") {
				t.Fatalf("width %d truncated result: %q", width, joined)
			}
			// Parent hook supplies the one-cell replacement for double-width
			// graphemes. At >=2 cells every row must fit with this test hook.
			if width >= 2 {
				for _, row := range rows {
					if ansi.StringWidth(row) > width {
						t.Fatalf("width %d overflow: %q", width, row)
					}
				}
			}
		}
	}
}

func TestWebEntriesPreserveUnexpectedShapesAndNestedMaps(t *testing.T) {
	nested := map[string]any{"title": "Title", "url": "https://example.test", "extra": "KEEP"}
	details := map[string]any{"sources": []any{nested, nil, "odd"}}
	input := session.Item{ToolName: "web_search", HasResult: true}
	input.Details, _ = json.Marshal(details)
	probe(40, testHooks(t)).WebEntries(details, "sources", "Sources", "title", "url")
	if nested["title"] != "Title" || nested["url"] != "https://example.test" || nested["extra"] != "KEEP" {
		t.Fatal("nested map mutated")
	}
	got := strings.Join(tooldetail.Render(input, config.Theme{}, 40, testHooks(t), toolrender.NewViews().FullDetail), "\n")
	for _, want := range []string{"Title", "https://example.test", "KEEP", "odd", "—"} {
		if !strings.Contains(got, want) {
			t.Fatal(got)
		}
	}
}
