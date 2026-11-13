package detail_test

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/core/session"
)

func TestFetchContentModesAndMetadata(t *testing.T) {
	for _, mode := range []string{"readable", "raw", "answer"} {
		item := session.Item{HasResult: true, Text: "first\n\n  raw 中文😀 last \n",
			Details: json.RawMessage(`{"urls":["https://example.test/中文"],"title":"Page title","urlCount":1,"successful":1,"mode":"` + mode + `","responseId":"fetch-9","truncated":false,"mimeType":"text/plain","totalBytes":9999,"shownBytes":100,"totalLines":50,"shownLines":3,"status":200,"future":[false,900719925474099312345]}`)}
		got := strings.Join(webTestRows(t, "fetch_content", item, 60, false), "\n")
		for _, want := range []string{"Sources", "https://example.test/中文", "Mode: " + mode, "Content ID: fetch-9", "HTTP status: 200", "Shown lines: 3", "900719925474099312345", "Content\n" + item.Text} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q: %s", want, got)
			}
		}
		if strings.Count(got, item.Text) != 1 {
			t.Fatal("duplicated content")
		}
	}
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
