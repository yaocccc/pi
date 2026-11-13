package hostui

import (
	"encoding/json"
	"pi-goui/internal/core/session"
	"strings"
	"testing"
)

func TestWorkerTaskProjectionCacheLegacyUpdates(t *testing.T) {
	item := session.Item{ID: "legacy", Role: "tool", ToolName: "worker", Arguments: json.RawMessage(`{"tasks":[{"taskId":"legacy-task","title":"Before","objective":"Keep objective"}]}`), Text: `{"results":[{"summary":["BEFORE_RESULT"]}]}`}
	cache := Inspector{Key: "id:legacy-task"}
	selected, ok := cache.Resolve(item)
	title := cache.Title
	if !ok || title != "Before" || !strings.Contains(string(selected.Details), "BEFORE_RESULT") {
		t.Fatal("legacy task not projected")
	}
	at := strings.Index(string(item.Arguments), "Before")
	copy(item.Arguments[at:], "After!")
	_, ok = cache.Resolve(item)
	title = cache.Title
	if !ok || title != "After!" {
		t.Fatal("in-place argument update was cached")
	}
	item.Text = `{"results":[{"summary":["AFTER_RESULT"]}]}`
	selected, ok = cache.Resolve(item)
	if !ok || !strings.Contains(string(selected.Details), "AFTER_RESULT") || strings.Contains(string(selected.Details), "BEFORE_RESULT") {
		t.Fatal("text-only result update was cached")
	}
	item.ToolName = "read"
	if _, ok := cache.Resolve(item); ok {
		t.Fatal("changed tool reused Worker projection")
	}
}
