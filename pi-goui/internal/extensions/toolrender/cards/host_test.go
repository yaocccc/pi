package cards

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/config"
	"pi-goui/internal/extensionapi"
	"pi-goui/internal/session"
)

func TestCardHostPrimitivesAreInstanceLocal(t *testing.T) {
	makeRenderer := func(marker string) *Renderer {
		return New(extensionapi.CardHost{
			Map: func(raw json.RawMessage) map[string]any { var m map[string]any; _ = json.Unmarshal(raw, &m); return m },
			String: func(m map[string]any, keys ...string) string {
				for _, key := range keys {
					if s, ok := m[key].(string); ok && s != "" {
						return s
					}
				}
				return ""
			},
			Paint: func(text, color string) string { return marker + text },
			Clean: func(text string) string { return text },
			Wrap:  func(text string, width int) []string { return []string{text} },
		})
	}
	item := session.Item{Arguments: json.RawMessage(`{"question":"Pick","options":["A","B"]}`), Details: json.RawMessage(`{"answer":"B"}`), HasResult: true}
	first, second := makeRenderer("first:"), makeRenderer("second:")
	for _, check := range []struct {
		renderer     *Renderer
		want, absent string
	}{{first, "first:", "second:"}, {second, "second:", "first:"}, {first, "first:", "second:"}} {
		text := strings.Join(check.renderer.QuestionRows(item, config.Theme{}, 80), "\n")
		if !strings.Contains(text, check.want+"Pick") || strings.Contains(text, check.absent) || !strings.Contains(text, "✓") {
			t.Fatalf("hooks leaked or question lost: %s", text)
		}
	}
}

func TestStableSlotCodesDoNotDependOnDisplayText(t *testing.T) {
	for _, code := range []string{"waiting_slot", "slot_acquired"} {
		if !WorkerSlotNotice(code) {
			t.Fatal(code)
		}
	}
	for _, code := range []string{"", "等待执行槽位", "已获得槽位", "slot_timeout", "failed"} {
		if WorkerSlotNotice(code) {
			t.Fatal(code)
		}
	}
}
