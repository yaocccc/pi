package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/session"
)

func TestGetSearchContentPageAndFindMetadata(t *testing.T) {
	item := session.Item{HasResult: true, Text: "page start\n\n  中文😀 last \n",
		Arguments: json.RawMessage(`{"responseId":"stored-1","findText":["中文","last"]}`),
		Details:   json.RawMessage(`{"url":"https://example.test","title":"Page","contentLength":10000,"offset":0,"limit":500,"returnedChars":50,"nextOffset":null,"truncated":false,"findMode":"case-insensitive","matches":[{"offset":900719925474099312345,"unknown":"MATCH_EXTRA"}],"future":"EXTRA"}`)}
	got := strings.Join(webTestRows(t, (*renderer).getSearchContent, item, 60, false), "\n")
	for _, want := range []string{"Response ID: stored-1", "Offset: 0", "Next offset: —", "Returned characters: 50", "Find passages", "中文", "900719925474099312345", "MATCH_EXTRA", "EXTRA", "Excerpt\n" + item.Text} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q: %s", want, got)
		}
	}
	if strings.Count(got, item.Text) != 1 {
		t.Fatal("repeated excerpt")
	}
}

func TestGetSearchContentNextPageRetained(t *testing.T) {
	item := session.Item{Text: "first page", Details: json.RawMessage(`{"responseId":"r","offset":0,"nextOffset":100,"truncated":true}`)}
	got := strings.Join(webTestRows(t, (*renderer).getSearchContent, item, 40, false), "\n")
	if !strings.Contains(got, "Next offset: 100") || !strings.Contains(got, "Upstream output truncated") {
		t.Fatal(got)
	}
}
