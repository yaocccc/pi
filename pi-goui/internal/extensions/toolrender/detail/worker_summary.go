package detail

import (
	"encoding/json"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// WorkerTaskResult resolves a stable task key against the latest saved snapshot.
func WorkerTaskResult(item session.Item, key string) (session.Item, string, bool) {
	if key != "" {
		for _, task := range WorkerTasks(item) {
			if task.Key == key {
				return task.Item, task.Title, true
			}
		}
	}
	return session.Item{}, "", false
}

// WorkerTaskSummaryRows renders an already-isolated task snapshot. The allowlist
// deliberately excludes transport IDs, contracts, raw payloads and activity logs;
// this is a clearly labelled fallback, not a reconstructed conversation.
func WorkerTaskSummaryRows(item session.Item, reason string, theme config.Theme, width int, line func(string, string, int) []string, clean func(string) string, renderResult func(session.Item, config.Theme, int) []string) []string {
	var data struct{ Tasks []map[string]json.RawMessage }
	_ = json.Unmarshal(item.Details, &data)
	selected := map[string]json.RawMessage{}
	if len(data.Tasks) == 1 {
		task := data.Tasks[0]
		for _, key := range []string{"title", "objective", "status", "mode", "requestedPreset", "resolvedPreset", "phase"} {
			if raw, ok := task[key]; ok {
				selected[key] = raw
			}
		}
		if _, ok := selected["requestedPreset"]; !ok {
			if raw, ok := task["preset"]; ok {
				selected["requestedPreset"] = raw
			}
		}
		var result map[string]json.RawMessage
		resultRaw := task["result"]
		var text string
		if json.Unmarshal(resultRaw, &text) == nil {
			resultRaw = json.RawMessage(text)
		}
		if json.Unmarshal(resultRaw, &result) == nil && result != nil {
			brief := map[string]json.RawMessage{}
			for _, key := range []string{"status", "summary", "validation", "acceptance", "changed_files", "findings", "risks", "error"} {
				if raw, ok := result[key]; ok {
					brief[key] = raw
				}
			}
			selected["result"], _ = json.Marshal(brief)
		} else if raw, ok := task["result"]; ok {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				selected["result"] = raw
			}
		}
	}
	brief := item
	brief.Arguments, brief.Text, brief.Images = nil, "", nil
	brief.Details, _ = json.Marshal(map[string]any{"tasks": []any{selected}})
	rows := line("简要详情 · 完整会话记录不可用", theme.Muted, max(1, width))
	rows = append(rows, line(clean(reason), theme.Muted, max(1, width))...)
	return append(rows, renderResult(brief, theme, width)...)
}
