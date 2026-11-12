package tooldetail

import (
	"strings"
	"testing"

	"pi-goui/internal/session"
)

func TestWebEnableRecordedCapabilities(t *testing.T) {
	details := decodeMap([]byte(`{"enabled":["web_search","中文😀 tool"],"unknown":{"count":9007199254740993123}}`))
	v := renderer{width: 40, hooks: testHooks(t)}
	v.webEnable(session.Item{HasResult: true, Text: "Enabled: web_search, 中文😀 tool."}, nil, details)
	got := strings.Join(v.rows, "\n")
	if !strings.Contains(got, "✓ web_search\n✓ 中文😀 tool") || strings.Contains(got, "Enabled:") {
		t.Fatal(got)
	}
	if details["unknown"] == nil {
		t.Fatal("discarded unknown metadata")
	}
}

func TestWebEnableUnavailableMalformedAndNoSuccessAssumption(t *testing.T) {
	for _, width := range []int{1, 2, 20, 40, 60} {
		v := renderer{width: width, hooks: testHooks(t)}
		details := decodeMap([]byte(`{"unavailable":["fetch_content"],"missing":["source_check"],"enabled":[{"custom":true}],"error":"activation failed"}`))
		v.webEnable(session.Item{Text: "\noriginal\n\n"}, nil, details)
		got := strings.Join(v.rows, "\n")
		for _, want := range []string{"× fetch_content", "○ source_check", "custom: yes", "activation failed", "\noriginal\n\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("width %d missing %q: %s", width, want, got)
			}
		}
	}
	v := renderer{width: 40, hooks: testHooks(t)}
	v.webEnable(session.Item{Running: true}, nil, map[string]any{})
	got := strings.Join(v.rows, "\n")
	if strings.Contains(got, "Enabled tools") || !strings.Contains(got, "Enabling configured tools") {
		t.Fatal(got)
	}
}
