package tooldetail

import (
	"strings"
	"testing"

	"pi-goui/internal/session"
)

func TestObsRecallHeaderPagingAndCompleteExcerpt(t *testing.T) {
	for _, width := range []int{1, 2, 20, 40, 60} {
		v := renderer{width: width, hooks: testHooks(t)}
		details := decodeMap([]byte(`{"id":"obs_abc","offset":0,"nextOffset":9007199254740993123,"bytes":9,"lines":3,"eof":false,"future":"retained"}`))
		header := "[obs_recall id=obs_abc offset=0 next_offset=9007199254740993123 eof=false]\n[chunk_bytes=9 chunk_lines=3; use next_offset to continue]\n"
		excerpt := "中文😀\n\n  tail \n"
		v.obsRecall(session.Item{HasResult: true, Text: header + excerpt}, decodeMap([]byte(`{"id":"obs_abc","offset":0}`)), details)
		got := strings.Join(v.rows, "\n")
		for _, want := range []string{"Byte offset\n0", "Next byte offset\n9007199254740993123", "More available", "Excerpt\n" + excerpt} {
			if !strings.Contains(got, want) {
				t.Errorf("width %d missing %q: %s", width, want, got)
			}
		}
		if strings.Contains(got, "[obs_recall") || strings.Contains(got, "Request") || details["future"] != "retained" {
			t.Fatal(got, details)
		}
	}
}

func TestObsRecallPreservesUnrecognizedHeadersAndFields(t *testing.T) {
	for _, raw := range []string{`{"eof":true,"nextOffset":0}`, `{"eof":"future","nextOffset":{"unknown":true}}`, `{}`} {
		text := "[obs_recall unknown newer header]\n\noriginal\n"
		details := decodeMap([]byte(raw))
		v := renderer{width: 40, hooks: testHooks(t)}
		v.obsRecall(session.Item{HasResult: true, Text: text}, nil, details)
		got := strings.Join(v.rows, "\n")
		if !strings.Contains(got, text) {
			t.Fatal("lost original header", got)
		}
		if raw == `{"eof":true,"nextOffset":0}` && !strings.Contains(got, "End of observation") {
			t.Fatal(got)
		}
		if raw == `{"eof":"future","nextOffset":{"unknown":true}}` && (details["eof"] != "future" || !strings.Contains(got, "unknown: yes")) {
			t.Fatal("lost malformed fields", got, details)
		}
	}
}
