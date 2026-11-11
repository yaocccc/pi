package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// Worker cards consume saved WorkerUiDetails, never the extension's live registry.
// A captured snapshot time freezes historical durations; missing metrics stay absent.
type workerCard struct {
	index                                                      int
	mode, objective, status, preset, model, sessionMode, phase string
	started, finished                                          float64
	usage, result                                              map[string]any
	activities                                                 []any
	toolCalls                                                  int
}
type workerView struct {
	cards               []workerCard
	status, batch, text string
	now                 float64
	errors              []string
	authoritative       bool
	result              map[string]any
}

func workerMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func workerArray(v any) []any        { a, _ := v.([]any); return a }
func workerNumber(m map[string]any, key string) float64 {
	if m[key] == nil {
		return 0
	}
	n, _ := strconv.ParseFloat(fmt.Sprint(m[key]), 64)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}
func workerFirst(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}
func workerCardFrom(m map[string]any, position int) workerCard {
	c := workerCard{toolCalls: int(workerNumber(m, "toolCalls")), index: position, mode: visualString(m, "mode"), objective: visualString(m, "objective"), status: visualString(m, "status"), preset: visualString(m, "resolvedPreset", "requestedPreset", "preset"), model: visualString(m, "modelId"), sessionMode: visualString(m, "sessionMode"), phase: visualString(m, "phase"), started: workerNumber(m, "startedAt"), finished: workerNumber(m, "finishedAt"), usage: workerMap(m["usage"]), result: workerMap(m["result"]), activities: workerArray(m["activities"])}
	if _, ok := m["index"]; ok {
		c.index = int(workerNumber(m, "index"))
	}
	execution := workerMap(c.result["execution"])
	c.model = workerFirst(visualString(execution, "actual_model_id"), c.model)
	if c.usage == nil {
		c.usage = workerMap(execution["usage"])
	}
	if status := visualString(c.result, "status"); status == "blocked" || status == "failed" || status == "cancelled" {
		c.status = status
	}
	return c
}
func workerData(item session.Item) workerView {
	args := visualMap(item.Arguments)
	details := visualMap(item.Details)
	v := workerView{batch: workerFirst(visualString(details, "batchId"), visualString(args, "batchId")), status: visualString(details, "status")}
	if visualString(details, "kind") == "worker-ui" {
		// Do not decode content at all when authoritative UI details are available.
		v.authoritative = true
		v.result = workerMap(details["payload"])
		v.status = workerFirst(v.status, visualString(v.result, "status"))
		v.now = workerNumber(details, "snapshotAt")
		if v.now == 0 {
			v.now = workerNumber(details, "finishedAt")
		}
		for i, value := range workerArray(details["tasks"]) {
			if m := workerMap(value); m != nil {
				v.cards = append(v.cards, workerCardFrom(m, i))
			}
		}
		for _, value := range workerArray(details["controlErrors"]) {
			if s := visualString(workerMap(value), "message"); s != "" {
				v.errors = append(v.errors, s)
			}
		}
		return v
	}
	root := visualMap([]byte(item.Text))
	v.result = root
	v.status = workerFirst(v.status, visualString(root, "status"))
	v.batch = workerFirst(v.batch, visualString(root, "batchId"))
	if root == nil {
		v.text = item.Text
	}
	taskArgs := workerArray(args["tasks"])
	if len(taskArgs) == 0 {
		if legacy := workerMap(args["task"]); legacy != nil {
			taskArgs = []any{legacy}
		}
	}
	tasks := workerArray(root["tasks"])
	result := workerMap(root["result"])
	results := workerArray(result["results"])
	if len(results) == 0 {
		results = workerArray(root["results"])
	}
	count := max(len(taskArgs), len(tasks), len(results))
	for i := 0; i < count; i++ {
		var a, state, res map[string]any
		if i < len(taskArgs) {
			a = workerMap(taskArgs[i])
		}
		if i < len(tasks) {
			state = workerMap(tasks[i])
		}
		if i < len(results) {
			res = workerMap(results[i])
		}
		c := workerCardFrom(a, i)
		c.result = res
		c.status = workerFirst(visualString(res, "status"), visualString(state, "status"), v.status)
		if c.status == "" {
			if item.Error {
				c.status = "failed"
			} else if item.Running {
				c.status = "queued"
			} else {
				c.status = "unknown"
			}
		}
		execution := workerMap(res["execution"])
		c.preset = workerFirst(visualString(execution, "resolved_preset", "requested_preset"), c.preset)
		c.model = visualString(execution, "actual_model_id", "resolved_model_id")
		c.sessionMode = workerFirst(visualString(execution, "session_mode"), c.sessionMode)
		c.usage = workerMap(execution["usage"])
		v.cards = append(v.cards, c)
	}
	return v
}
func workerStatus(status string, theme config.Theme) (string, string) {
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
func (v workerView) overall(item session.Item) string {
	if item.Error {
		return "failed"
	}
	// Explicit terminal failures outrank an outer tool invocation's successful ACK.
	for _, status := range []string{"failed", "blocked", "cancelled", "canceled", "cancelling", "running", "queued"} {
		if v.status == status {
			return status
		}
		for _, c := range v.cards {
			if c.status == status {
				return status
			}
		}
	}
	if len(v.cards) > 0 {
		for _, c := range v.cards {
			if c.status != "completed" && c.status != "passed" {
				return "unknown"
			}
		}
		return "completed"
	}
	if v.status != "" {
		return v.status
	}
	if item.Running {
		return "running"
	}
	return "unknown"
}
func workerHeader(item session.Item, v workerView, expanded bool, theme config.Theme, width int) string {
	width = min(width, 80)
	arrow := "›"
	if expanded {
		arrow = "⌄"
	}
	status := v.overall(item)
	icon, color := workerStatus(status, theme)
	label := "worker"
	if len(v.cards) > 0 {
		label += fmt.Sprintf(" · %d tasks", len(v.cards))
	}
	label += " · " + status
	if v.batch != "" {
		label += " · " + oneLine(v.batch)
	}
	header := paint(arrow, theme.Muted) + " " + paint(icon, color) + " " + paint(label, theme.Text)
	return withToolBadge(header, tokenBadge(item.EstimatedTokens, theme), width, 24, false)
}
func workerDuration(c workerCard, now float64) string {
	end := c.finished
	if end == 0 {
		end = now
	}
	if c.started <= 0 || end < c.started || end <= 0 {
		return ""
	}
	seconds := int(math.Round((end - c.started) / 1000))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
}
func workerTokens(n float64) string {
	if n >= 1e6 {
		return fmt.Sprintf("%.1fM", n/1e6)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", n/1000)
	}
	return fmt.Sprintf("%.0f", n)
}
func workerRuntime(c workerCard, now float64) string {
	var parts []string
	if c.preset != "" {
		parts = append(parts, c.preset)
	}
	if n := workerNumber(c.usage, "turns"); n > 0 {
		parts = append(parts, fmt.Sprintf("%.0f turns", n))
	}
	var usage []string
	if n := workerNumber(c.usage, "input"); n > 0 {
		usage = append(usage, "↑"+workerTokens(n))
	}
	if n := workerNumber(c.usage, "output"); n > 0 {
		usage = append(usage, "↓"+workerTokens(n))
	}
	if len(usage) > 0 {
		parts = append(parts, strings.Join(usage, " "))
	}
	if duration := workerDuration(c, now); duration != "" {
		parts = append(parts, duration)
	}
	return oneLine(strings.Join(parts, " · "))
}

func workerTaskID(item session.Item, index int, position int) string {
	// Native indices are stable; position disambiguates malformed duplicate indices.
	return fmt.Sprintf("worker-task:%s:%d:%d", item.ID, index, position)
}
func workerSlotNotice(text string) bool {
	if strings.Contains(text, "超时") || strings.Contains(text, "取消") || strings.Contains(text, "失败") {
		return false
	}
	return strings.Contains(text, "等待执行槽位") || strings.Contains(text, "等待槽位") || strings.Contains(text, "已获得槽位")
}

func workerDetailRows(item session.Item, theme config.Theme, _ Options, width int) ([]string, []Block) {
	width = max(1, width)
	v := workerData(item)
	var rows []string
	var blocks []Block
	var separators []int
	line := func(s, color string) { rows = append(rows, visualWrap(paint(Clean(s), color), width)...) }
	for _, err := range v.errors {
		line("Control error: "+err, theme.Error)
	}
	for i, c := range v.cards {
		if i > 0 {
			separators = append(separators, len(rows))
			rows = append(rows, "")
		}
		start := len(rows)
		icon, color := workerStatus(c.status, theme)
		header := oneLine(workerFirst(c.mode, "task"))
		if info := workerRuntime(c, v.now); info != "" {
			header += " · " + info
		}
		rows = append(rows, fit(paint(icon, color)+" "+paint(header, theme.Text), width))
		section := func(marker, shade string, lines []string) {
			if len(lines) == 0 {
				return
			}
			first := true
			for _, text := range lines {
				// On tiny terminals omit decoration rather than losing content width.
				prefixWidth := 2
				if marker != "" {
					prefixWidth = 4
				}
				if width <= prefixWidth {
					prefixWidth = 0
				}
				for _, row := range visualWrap(paint(Clean(text), theme.Text), width-prefixWidth) {
					prefix := ""
					if prefixWidth > 0 {
						prefix = strings.Repeat(" ", prefixWidth)
						if first && marker != "" {
							prefix = "  " + paint(marker, shade) + " "
						}
					}
					rows = append(rows, prefix+row)
					first = false
				}
			}
		}
		if c.objective != "" {
			section("◦", theme.Accent, []string{c.objective})
		}
		steps := workerToolRows(c, theme, width)
		rows = append(rows, steps...)
		var conclusion []string
		workerResult(map[string]any{"summary": c.result["summary"]}, func(s, _ string) {
			if s != "结果:" {
				conclusion = append(conclusion, s)
			}
		}, theme)
		if len(conclusion) > 0 {
			section("", theme.Text, conclusion)
		} else {
			// Tool and thinking activities already appear in the steps list.
			var progress []string
			for _, value := range c.activities {
				a := workerMap(value)
				kind := visualString(a, "type")
				if kind != "text" && kind != "message" && kind != "phase" {
					continue
				}
				text := visualString(a, "detail")
				if kind == "phase" {
					label := visualString(a, "label")
					if visualString(a, "status") != "failed" && (workerSlotNotice(label) || workerSlotNotice(text)) {
						continue
					}
					text = workerFirst(text, label)
				}
				if strings.TrimSpace(text) != "" {
					progress = append(progress, text)
				}
			}
			if len(progress) == 0 && len(steps) == 0 && !workerSlotNotice(c.phase) {
				switch c.status {
				case "running", "cancelling":
					progress = append(progress, workerFirst(c.phase, "正在执行中..."))
				case "queued":
					progress = append(progress, workerFirst(c.phase, "等待执行..."))
				}
			}
			workerResult(map[string]any{"error": c.result["error"]}, func(s, _ string) {
				if s != "Error" {
					progress = append(progress, s)
				}
			}, theme)
			section("", theme.Muted, progress)
		}
		blocks = append(blocks, Block{ID: workerTaskID(item, c.index, i), Role: "workerTask", MessageIndex: item.MessageIndex, Start: start, End: len(rows), Expanded: true})
	}
	if len(v.cards) == 0 || v.result["summary"] != nil || v.result["error"] != nil || v.result["validation_errors"] != nil {
		workerResult(v.result, line, theme)
	}
	if !v.authoritative && v.text != "" {
		line(v.text, theme.Text)
	}
	if len(rows) == 0 {
		line("No task details available", theme.Muted)
	}
	// Size separators from real content, so they never stretch the card.
	contentWidth := 1
	for _, row := range rows {
		contentWidth = max(contentWidth, ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " \t")))
	}
	corners, _ := frameGradient(theme.FramePreset, "worker")
	c := corners[0]
	separatorColor := fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
	for _, index := range separators {
		rows[index] = paint(strings.Repeat("─", min(width, contentWidth)), separatorColor)
	}
	return rows, blocks
}
func workerResult(result map[string]any, line func(string, string), theme config.Theme) {
	for _, field := range []struct{ key, label string }{{"summary", "结果:"}, {"changed_files", "Files"}, {"validation", "Verification"}, {"acceptance", "Acceptance"}, {"findings", "Findings"}, {"risks", "Risks"}, {"error", "Error"}, {"validation_errors", "Validation errors"}, {"message", "Message"}, {"reason", "Reason"}, {"recommended_next_action", "Next action"}, {"out_of_scope", "Out of scope"}} {
		value, ok := result[field.key]
		if !ok || value == nil {
			continue
		}
		values := workerArray(value)
		if values == nil {
			values = []any{value}
		}
		if len(values) == 0 {
			continue
		}
		line(field.label, theme.Accent)
		for _, v := range values {
			if m := workerMap(v); m != nil {
				// Only meaningful result fields; never serialize arbitrary execution objects.
				var parts []string
				for _, key := range []string{"severity", "file", "path", "location", "command", "criterion", "result", "problem", "evidence", "details", "message", "recommendation", "errors"} {
					if s, ok := m[key].(string); ok && s != "" {
						parts = append(parts, s)
					} else if list := workerArray(m[key]); len(list) > 0 {
						for _, x := range list {
							if s, ok := x.(string); ok {
								parts = append(parts, s)
							}
						}
					}
				}
				if len(parts) > 0 {
					line("• "+strings.Join(parts, " · "), theme.Text)
				}
			} else if s, ok := v.(string); ok && s != "" {
				line("• "+s, theme.Text)
			}
		}
	}
}
func workerLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	width = max(1, width)
	expanded := isExpanded(item.ID, opts)
	v := workerData(item)
	rows := []string{workerHeader(item, v, expanded, theme, width)}
	blocks := []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: 1, Foldable: true, Expanded: expanded}}
	if !expanded {
		return rows, blocks
	}
	contentWidth := width
	offset := 1
	if width >= 8 {
		contentWidth = width - 4
		offset++
	}
	detail, children := workerDetailRows(item, theme, opts, contentWidth)
	if width >= 8 {
		title := "worker"
		var cardWidth int
		detail, cardWidth = compactBubble(detail, title, width)
		detail = frame(detail, title, cardWidth, "worker", false, width, theme)
	}
	rows = append(rows, detail...)
	for _, b := range children {
		b.Start += offset
		b.End += offset
		blocks = append(blocks, b)
	}
	return rows, blocks
}
func toolLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	if visualName(item.ToolName) == "worker" {
		return workerLayout(item, theme, opts, width)
	}
	rows := toolBody(item, theme, opts, width)
	return rows, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(rows), Foldable: true, Expanded: isExpanded(item.ID, opts)}}
}
