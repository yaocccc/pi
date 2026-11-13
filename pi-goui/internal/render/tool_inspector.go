package render

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	frames "pi-goui/internal/render/frame"
	"pi-goui/internal/render/tooldetail"
	"pi-goui/internal/session"
)

// ToolResultRows is the unabridged, display-only inspector view. It deliberately
// bypasses transcript snippets, tail limits and single-line code truncation.
func ToolResultRows(item session.Item, theme config.Theme, width int) []string {
	return tooldetail.Render(item, theme, width, tooldetail.Hooks{
		Line: func(text, color string, width int) []string {
			var rows []string
			// Style source lines separately: multiline styling pads short rows.
			for _, line := range strings.Split(cleanCode(text), "\n") {
				rows = append(rows, visualWrap(paint(line, color), width)...)
			}
			return rows
		},
		Clean: Clean, CodeRows: codeRows, DiffRows: diffRows, DiffCounts: diffCounts,
	})
}

// WorkerTaskResult resolves a stable task key against the latest saved snapshot.
func WorkerTaskResult(item session.Item, key string) (session.Item, string, bool) {
	if key != "" {
		for _, task := range tooldetail.WorkerTasks(item) {
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
func WorkerTaskSummaryRows(item session.Item, reason string, theme config.Theme, width int) []string {
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
	rows := visualWrap(paint("简要详情 · 完整会话记录不可用", theme.Muted), max(1, width))
	rows = append(rows, visualWrap(paint(Clean(reason), theme.Muted), max(1, width))...)
	return append(rows, ToolResultRows(brief, theme, width)...)
}

// ToolResultFrame uses the sidebar's square corners, padding and gradient.
// The caller supplies interior rows, including its title and status rows.
func ToolResultFrame(content []string, width int, theme config.Theme) []string {
	width = max(1, width)
	border := func(left, right string) string {
		if width == 1 {
			return left
		}
		return left + strings.Repeat("─", width-2) + right
	}
	rows := []string{border("┌", "┐")}
	for _, line := range content {
		if width == 1 {
			rows = append(rows, "│")
			continue
		}
		pad := ""
		if width >= 4 {
			pad = " "
		}
		inner := width - 2 - 2*len(pad)
		line = fit(line, inner)
		rows = append(rows, "│"+pad+line+strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)))+pad+"│")
	}
	rows = append(rows, border("└", "┘"))
	return frames.Gradient("sidebar", rows, width, theme)
}

func ToolResultTitle(item session.Item) string {
	path := visualString(visualMap(item.Arguments), "path")
	return strings.TrimSpace(oneLine(item.ToolName + " " + path))
}
