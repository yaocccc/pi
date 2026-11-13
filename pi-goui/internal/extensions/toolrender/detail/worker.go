package detail

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// WorkerTask is a display-only, isolated task snapshot. Key is scoped to its
// parent tool call and never depends on the task's current array position.
type WorkerTask struct {
	Key, Title string
	Item       session.Item
}

// WorkerTasks follows the same authoritative-snapshot/legacy-result precedence
// as the transcript. Ambiguous identities deliberately have no inspect action.
func WorkerTasks(item session.Item) []WorkerTask {
	if toolName(item.ToolName) != "worker" {
		return nil
	}
	details := decodeMap(item.Details)
	args := decodeMap(item.Arguments)
	root := decodeMap([]byte(item.Text))
	authoritative := stringField(details, "kind") == "worker-ui"
	var tasks []any
	if authoritative {
		tasks, _ = details["tasks"].([]any)
	} else {
		inputs, _ := args["tasks"].([]any)
		if len(inputs) == 0 {
			if legacy, ok := args["task"].(map[string]any); ok {
				inputs = []any{legacy}
			}
		}
		states, _ := root["tasks"].([]any)
		result, _ := root["result"].(map[string]any)
		results, _ := result["results"].([]any)
		if len(results) == 0 {
			results, _ = root["results"].([]any)
		}
		for i := 0; i < max(len(inputs), len(states), len(results)); i++ {
			task := map[string]any{}
			if i < len(inputs) {
				if a, ok := inputs[i].(map[string]any); ok {
					task = copyMap(a)
				}
			}
			if i < len(states) {
				if s, ok := states[i].(map[string]any); ok {
					for k, value := range s {
						task[k] = value
					}
				}
			}
			if i < len(results) {
				task["result"] = results[i]
			}
			if _, ok := task["status"]; !ok {
				if item.Running {
					task["status"] = "queued"
				} else {
					task["status"] = "unknown"
				}
			}
			tasks = append(tasks, task)
		}
	}
	batch := stringField(details, "batchId")
	if !authoritative {
		batch = stringField(root, "batchId")
	}
	var out []WorkerTask
	counts := map[string]int{}
	for _, value := range tasks {
		task, ok := value.(map[string]any)
		if !ok {
			continue
		}
		key := ""
		if id := stringField(task, "taskId"); id != "" {
			key = "id:" + id
		} else if authoritative {
			// WorkerUiTask.index is the execution's immutable native index,
			// unlike its position in a reordered snapshot array.
			if index, ok := task["index"].(json.Number); ok {
				if n, err := index.Int64(); err == nil && n >= 0 {
					key = "index:" + batch + ":" + index.String()
				}
			}
		} else if objective := stringField(task, "objective"); objective != "" {
			identity, _ := json.Marshal([]string{stringField(task, "mode"), objective, stringField(task, "title")})
			key = fmt.Sprintf("definition:%x", sha256.Sum256(identity))
		}
		title := strings.TrimSpace(stringField(task, "title"))
		if title == "" {
			title = stringField(task, "objective")
		}
		if title == "" {
			title = stringField(task, "taskId")
		}
		if title == "" {
			title = "Task"
		}
		isolated := map[string]any{"kind": "worker-ui", "tasks": []any{task}}
		// Only timing shared by the batch is relevant to a task; never copy
		// payload/text/controlErrors containing other tasks' private results.
		if now, ok := details["snapshotAt"]; ok {
			isolated["snapshotAt"] = now
		}
		raw, _ := json.Marshal(isolated)
		selected := item
		selected.Text, selected.Arguments, selected.Details = "", nil, raw
		selected.Images, selected.Error = nil, false
		out = append(out, WorkerTask{Key: key, Title: title, Item: selected})
		if key != "" {
			counts[key]++
		}
	}
	for i := range out {
		if counts[out[i].Key] != 1 {
			out[i].Key = ""
		}
	}
	return out
}

func (v *Renderer) Worker(item session.Item, args, details map[string]any) {
	// Capture the original payload before replacing exact result copies
	// with task references in the display-only details map.
	payload, _ := json.Marshal(details["payload"])
	v.workers(details)
	// Only omit exact semantic duplicates, never arbitrary messages.
	if !sameJSON(item.Text, item.Details) && !sameJSON(item.Text, payload) {
		if value, ok := decodeJSON([]byte(item.Text)); ok {
			v.Heading("Output")
			v.Value(value, "")
		} else {
			v.Output(item.Text)
		}
	}
}

func sameJSON(text string, raw json.RawMessage) bool {
	a, aok := decodeJSON([]byte(text))
	b, bok := decodeJSON(raw)
	if !aok || !bok {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (v *Renderer) workers(details map[string]any) {
	tasks, ok := details["tasks"].([]any)
	if !ok {
		return
	}
	v.Heading(fmt.Sprintf("Tasks · %d", len(tasks)))
	for i, value := range tasks {
		task, ok := value.(map[string]any)
		if !ok {
			v.Value(value, "")
			continue
		}
		task = copyMap(task)
		if i > 0 {
			v.Line(strings.Repeat("─", min(v.Width, 24)), v.Theme.Muted)
		}
		status := taskStatus(task)
		icon, color := taskStatusStyle(status, v.Theme)
		v.Line(fmt.Sprintf("%s Task %d · %s", icon, i+1, status), color)
		if task["status"] == status {
			delete(task, "status")
		}
		if duration := taskDuration(task, numberField(details, "snapshotAt")); duration != "" {
			v.Line("Duration · "+duration, v.Theme.Muted)
		}
		v.Field(task, "title", "◦ Title")
		v.Field(task, "objective", "Objective")
		v.workerMetadata(task)
		v.Field(task, "phase", "⋯ Progress")
		v.workerUsage(task)
		if activities, ok := task["activities"].([]any); ok {
			v.Heading(fmt.Sprintf("Activity · %d", len(activities)))
			for index, activity := range activities {
				v.workerActivity(activity, index == len(activities)-1)
			}
			delete(task, "activities")
		}
		v.workerResult(task)
		if len(task) > 0 {
			v.Heading("Task details")
			v.Value(task, "")
		}
	}
	// Final snapshots repeat task results in payload.result.results. Keep the
	// envelope and unknown fields, but link exact copies to the sections above.
	if payload, ok := details["payload"].(map[string]any); ok {
		payload = copyMap(payload)
		if result, ok := payload["result"].(map[string]any); ok {
			result = copyMap(result)
			if values, ok := result["results"].([]any); ok {
				values = append([]any(nil), values...)
				for i, value := range values {
					if i >= len(tasks) {
						break
					}
					if task, ok := tasks[i].(map[string]any); ok {
						if saved, ok := task["result"]; ok {
							a, _ := json.Marshal(saved)
							b, _ := json.Marshal(value)
							if string(a) == string(b) {
								values[i] = fmt.Sprintf("See Task %d result above", i+1)
							}
						}
					}
				}
				result["results"] = values
			}
			payload["result"] = result
		}
		details["payload"] = payload
	}
	delete(details, "tasks")
}

// Keep exact values and source keys in compact metadata rows. Unknown fields
// remain in the copied task and are rendered by the normal fallback below.
func (v *Renderer) workerMetadata(task map[string]any) {
	var parts []string
	for _, key := range []string{"mode", "requestedPreset", "resolvedPreset", "sessionMode", "modelId", "thinking"} {
		if value, ok := task[key].(string); ok {
			parts = append(parts, key+": "+value)
			delete(task, key)
		}
	}
	if len(parts) > 0 {
		v.Line(strings.Join(parts, " · "), v.Theme.Muted)
	}
}

func (v *Renderer) workerUsage(task map[string]any) {
	usage, ok := task["usage"].(map[string]any)
	if !ok {
		v.Field(task, "usage", "Usage")
		return
	}
	usage = copyMap(usage)
	v.Heading("Usage")
	// Match the transcript's ↑/↓ token vocabulary, retaining exact values.
	var parts []string
	for _, key := range []string{"input", "output", "turns", "cacheRead", "cacheWrite", "contextTokens"} {
		value, exists := usage[key]
		if !exists {
			continue
		}
		switch value.(type) {
		case json.Number, float64:
			prefix := ""
			if key == "input" {
				prefix = "↑"
			} else if key == "output" {
				prefix = "↓"
			}
			parts = append(parts, prefix+key+": "+v.Scalar(value))
			delete(usage, key)
		}
	}
	if len(parts) > 0 {
		v.Line(strings.Join(parts, " · "), v.Theme.Muted)
	}
	if len(usage) > 0 || len(parts) == 0 {
		v.Value(usage, "")
	}
	delete(task, "usage")
}

func (v *Renderer) workerActivity(value any, last bool) {
	activity, ok := value.(map[string]any)
	if !ok {
		v.Value(value, "")
		return
	}
	activity = copyMap(activity)
	status := stringField(activity, "status")
	icon, color := taskStatusStyle(status, v.Theme)
	label := stringField(activity, "label")
	if status == "" && stringField(activity, "type") == "thinking" {
		icon, color = "!", v.Theme.Thinking
	}
	branch, continuation := "├ ", "│ "
	if last {
		branch, continuation = "└ ", "  "
	}
	if label == "" {
		label = stringField(activity, "type")
	}
	v.workerIndented(branch, strings.TrimSpace(icon+" "+label), color)
	if _, ok := activity["label"].(string); ok {
		delete(activity, "label")
	}
	// WorkerUiActivity transport bookkeeping is not activity content. Status
	// and kind are already conveyed by the icon/label; never filter strings
	// inside detail or result, which may legitimately contain these words.
	for _, key := range []string{"id", "at", "index", "contextSizeEstimate", "status", "type"} {
		delete(activity, key)
	}
	if detail, ok := activity["detail"].(string); ok {
		// A hanging gutter connects multiline tool/thinking output to its
		// activity, just as the main Worker card does, without clipping it.
		v.workerIndented(continuation, detail, v.Theme.Text)
		delete(activity, "detail")
	}
	if len(activity) > 0 {
		v.Value(activity, "")
	}
}

func (v *Renderer) workerResult(task map[string]any) {
	result, ok := task["result"].(map[string]any)
	if !ok {
		v.Field(task, "result", "Result")
		return
	}
	result = copyMap(result)
	v.Heading("Result")
	for _, field := range [][2]string{{"summary", "Summary"}, {"validation", "Validation"}, {"acceptance", "Acceptance"}, {"changed_files", "Changed files"}, {"findings", "Findings"}, {"risks", "Risks"}, {"error", "Error"}} {
		if field[0] == "validation" || field[0] == "acceptance" {
			v.workerChecks(result, field[0], field[1])
		} else {
			v.Field(result, field[0], field[1])
		}
	}
	if len(result) > 0 || len(task["result"].(map[string]any)) == 0 {
		v.Value(result, "")
	}
	delete(task, "result")
}

// Preserve complete text while maintaining a hanging gutter across wraps.
func (v *Renderer) workerIndented(prefix, text, color string) {
	w := ansi.StringWidth(prefix)
	if v.Width <= w+1 {
		v.Line(text, color)
		return
	}
	for i, row := range v.Hooks.Line(text, color, v.Width-w) {
		gutter := prefix
		if i > 0 && prefix == "├ " {
			gutter = "│ "
		} else if i > 0 && prefix == "└ " {
			gutter = "  "
		}
		(*v.Rows) = append((*v.Rows), gutter+row)
	}
}

func (v *Renderer) workerChecks(result map[string]any, key, label string) {
	checks, ok := result[key].([]any)
	if !ok || len(checks) == 0 {
		v.Field(result, key, label)
		return
	}
	v.Heading(label)
	for _, value := range checks {
		check, ok := value.(map[string]any)
		if !ok {
			v.Value(value, "")
			continue
		}
		check = copyMap(check)
		status := stringField(check, "result")
		if status != "" {
			icon, color := taskStatusStyle(status, v.Theme)
			v.Line(icon+" "+status, color)
			delete(check, "result")
		}
		for _, key := range []string{"command", "criterion", "details", "evidence"} {
			if text, ok := check[key].(string); ok {
				v.workerIndented("  ", key+": "+text, v.Theme.Text)
				delete(check, key)
			}
		}
		if len(check) > 0 {
			v.Value(check, "  ")
		}
	}
	delete(result, key)
}

func taskStatusStyle(status string, theme config.Theme) (string, string) {
	switch status {
	case "completed", "passed":
		return "✓", theme.Success
	case "running", "cancelling":
		return "⋯", theme.Warning
	case "blocked":
		return "!", theme.Warning
	case "failed":
		return "✗", theme.Error
	case "cancelled", "canceled":
		return "■", theme.Muted
	case "queued":
		return "○", theme.Muted
	default:
		return "?", theme.Muted
	}
}
func numberField(m map[string]any, key string) float64 {
	if m[key] == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(fmt.Sprint(m[key]), 64)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}
func taskDuration(task map[string]any, now float64) string {
	end := numberField(task, "finishedAt")
	if end == 0 {
		end = now
	}
	if numberField(task, "startedAt") <= 0 || end < numberField(task, "startedAt") || end <= 0 {
		return ""
	}
	seconds := int(math.Round((end - numberField(task, "startedAt")) / 1000))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
}

func taskStatus(task map[string]any) string {
	status := stringField(task, "status")
	result, _ := task["result"].(map[string]any)
	if terminal := stringField(result, "status"); terminal == "blocked" || terminal == "failed" || terminal == "cancelled" || terminal == "canceled" {
		status = terminal
	}
	return status
}
