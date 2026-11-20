package detail_test

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestWebContentMetadataAndCompleteText(t *testing.T) {
	check := func(t *testing.T, tool string, item session.Item, want ...string) {
		t.Helper()
		got := strings.Join(webTestRows(t, tool, item, 60, false), "\n")
		for _, fragment := range want {
			if !strings.Contains(got, fragment) {
				t.Errorf("missing %q: %s", fragment, got)
			}
		}
		if n := strings.Count(got, item.Text); n != 1 {
			t.Fatalf("content appears %d times, want 1", n)
		}
	}
	for _, mode := range []string{"readable", "raw", "answer"} {
		t.Run("fetch/"+mode, func(t *testing.T) {
			item := session.Item{HasResult: true, Text: "first\n\n  raw 中文😀 last \n",
				Details: json.RawMessage(`{"urls":["https://example.test/中文"],"title":"Page title","urlCount":1,"successful":1,"mode":"` + mode + `","responseId":"fetch-9","truncated":false,"mimeType":"text/plain","totalBytes":9999,"shownBytes":100,"totalLines":50,"shownLines":3,"status":200,"future":[false,900719925474099312345]}`)}
			check(t, "fetch_content", item, "Sources", "https://example.test/中文", "Mode: "+mode, "Content ID: fetch-9", "HTTP status: 200", "Shown lines: 3", "900719925474099312345", "Content\n"+item.Text)
		})
	}
	t.Run("stored-page", func(t *testing.T) {
		stored := session.Item{HasResult: true, Text: "page start\n\n  中文😀 last \n",
			Arguments: json.RawMessage(`{"responseId":"stored-1","findText":["中文","last"]}`),
			Details:   json.RawMessage(`{"url":"https://example.test","title":"Page","contentLength":10000,"offset":0,"limit":500,"returnedChars":50,"nextOffset":null,"truncated":false,"findMode":"case-insensitive","matches":[{"offset":900719925474099312345,"unknown":"MATCH_EXTRA"}],"future":"EXTRA"}`)}
		check(t, "get_search_content", stored, "Response ID: stored-1", "Offset: 0", "Next offset: —", "Returned characters: 50", "Find passages", "中文", "900719925474099312345", "MATCH_EXTRA", "EXTRA", "Excerpt\n"+stored.Text)
	})
}

func TestFetchPendingAndPartialFailureNotCalledSuccessful(t *testing.T) {
	item := session.Item{Running: true, Arguments: json.RawMessage(`{"url":"https://pending.test"}`)}
	got := strings.Join(webTestRows(t, "fetch_content", item, 60, false), "\n")
	if !strings.Contains(got, "Fetch · running") || !strings.Contains(got, "https://pending.test") {
		t.Fatal(got)
	}
	item.Running, item.HasResult = false, true
	item.Details = json.RawMessage(`{"urls":["https://pending.test"],"successful":0,"error":"network failed"}`)
	got = strings.Join(webTestRows(t, "fetch_content", item, 60, false), "\n")
	if !strings.Contains(got, "Succeeded: 0") || !strings.Contains(got, "network failed") || strings.Contains(got, "success ·") {
		t.Fatal(got)
	}
}

func TestGetSearchContentNextPageRetained(t *testing.T) {
	item := session.Item{Text: "first page", Details: json.RawMessage(`{"responseId":"r","offset":0,"nextOffset":100,"truncated":true}`)}
	got := strings.Join(webTestRows(t, "get_search_content", item, 40, false), "\n")
	if !strings.Contains(got, "Next offset: 100") || !strings.Contains(got, "Upstream output truncated") {
		t.Fatal(got)
	}
}
