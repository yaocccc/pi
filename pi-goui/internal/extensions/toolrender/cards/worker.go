package cards

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/core/config"
	frames "pi-goui/internal/core/render/frame"
	"pi-goui/internal/core/session"
	detailview "pi-goui/internal/extensions/toolrender/detail"
)

// Worker cards consume saved WorkerUiDetails, never the extension's live registry.
// A captured snapshot time freezes historical durations; missing metrics stay absent.
type WorkerCard struct {
	Index                                                                        int
	Mode, Title, Objective, Status, Preset, Model, SessionMode, Phase, PhaseCode string
	Started, Finished                                                            float64
	Usage, Result                                                                map[string]any
	Activities                                                                   []any
	ToolCalls                                                                    int
}
type WorkerView struct {
	Cards               []WorkerCard
	Status, Batch, Text string
	Now                 float64
	Errors              []string
	Authoritative       bool
	Result              map[string]any
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
func (r *Renderer) WorkerCardFrom(m map[string]any, position int) WorkerCard {
	c := WorkerCard{ToolCalls: int(workerNumber(m, "toolCalls")), Index: position, Mode: r.String(m, "mode"), Title: r.String(m, "title"), Objective: r.String(m, "objective"), Status: r.String(m, "status"), Preset: r.String(m, "resolvedPreset", "requestedPreset", "preset"), Model: r.String(m, "modelId"), SessionMode: r.String(m, "sessionMode"), Phase: r.String(m, "phase"), PhaseCode: r.String(m, "phaseCode"), Started: workerNumber(m, "startedAt"), Finished: workerNumber(m, "finishedAt"), Usage: workerMap(m["usage"]), Result: workerMap(m["result"]), Activities: workerArray(m["activities"])}
	if _, ok := m["index"]; ok {
		c.Index = int(workerNumber(m, "index"))
	}
	execution := workerMap(c.Result["execution"])
	c.Model = workerFirst(r.String(execution, "actual_model_id"), c.Model)
	if c.Usage == nil {
		c.Usage = workerMap(execution["usage"])
	}
	if status := r.String(c.Result, "status"); status == "blocked" || status == "failed" || status == "cancelled" {
		c.Status = status
	}
	return c
}
func (r *Renderer) WorkerData(item session.Item) WorkerView {
	args := r.Map(item.Arguments)
	details := r.Map(item.Details)
	v := WorkerView{Batch: workerFirst(r.String(details, "batchId"), r.String(args, "batchId")), Status: r.String(details, "status")}
	if r.String(details, "kind") == "worker-ui" {
		// Do not decode content at all when authoritative UI details are available.
		v.Authoritative = true
		v.Result = workerMap(details["payload"])
		v.Status = workerFirst(v.Status, r.String(v.Result, "status"))
		v.Now = workerNumber(details, "snapshotAt")
		if v.Now == 0 {
			v.Now = workerNumber(details, "finishedAt")
		}
		for i, value := range workerArray(details["tasks"]) {
			if m := workerMap(value); m != nil {
				v.Cards = append(v.Cards, r.WorkerCardFrom(m, i))
			}
		}
		for _, value := range workerArray(details["controlErrors"]) {
			if s := r.String(workerMap(value), "message"); s != "" {
				v.Errors = append(v.Errors, s)
			}
		}
		return v
	}
	root := r.Map([]byte(item.Text))
	v.Result = root
	v.Status = workerFirst(v.Status, r.String(root, "status"))
	v.Batch = workerFirst(v.Batch, r.String(root, "batchId"))
	if root == nil {
		v.Text = item.Text
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
		c := r.WorkerCardFrom(a, i)
		c.Result = res
		c.Status = workerFirst(r.String(res, "status"), r.String(state, "status"), v.Status)
		if c.Status == "" {
			if item.Error {
				c.Status = "failed"
			} else if item.Running {
				c.Status = "queued"
			} else {
				c.Status = "unknown"
			}
		}
		execution := workerMap(res["execution"])
		c.Preset = workerFirst(r.String(execution, "resolved_preset", "requested_preset"), c.Preset)
		c.Model = r.String(execution, "actual_model_id", "resolved_model_id")
		c.SessionMode = workerFirst(r.String(execution, "session_mode"), c.SessionMode)
		c.Usage = workerMap(execution["usage"])
		v.Cards = append(v.Cards, c)
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
func (v WorkerView) overall(item session.Item) string {
	if item.Error {
		return "failed"
	}
	// Explicit terminal failures outrank an outer tool invocation's successful ACK.
	for _, status := range []string{"failed", "blocked", "cancelled", "canceled", "cancelling", "running", "queued"} {
		if v.Status == status {
			return status
		}
		for _, c := range v.Cards {
			if c.Status == status {
				return status
			}
		}
	}
	if len(v.Cards) > 0 {
		for _, c := range v.Cards {
			if c.Status != "completed" && c.Status != "passed" {
				return "unknown"
			}
		}
		return "completed"
	}
	if v.Status != "" {
		return v.Status
	}
	if item.Running {
		return "running"
	}
	return "unknown"
}
func (r *Renderer) WorkerHeader(item session.Item, v WorkerView, expanded bool, theme config.Theme, width int) string {
	width = min(width, 80)
	arrow := "›"
	if expanded {
		arrow = "⌄"
	}
	status := v.overall(item)
	icon, color := workerStatus(status, theme)
	label := "worker"
	if len(v.Cards) > 0 {
		label += fmt.Sprintf(" · %d tasks", len(v.Cards))
	}
	label += " · " + status
	if v.Batch != "" {
		label += " · " + r.OneLine(v.Batch)
	}
	header := r.Paint(arrow, theme.Muted) + " " + r.Paint(icon, color) + " " + r.Paint(label, theme.Text)
	return r.Badge(header, r.Tokens(item.EstimatedTokens, theme), width, 24, false)
}
func WorkerDuration(c WorkerCard, now float64) string {
	end := c.Finished
	if end == 0 {
		end = now
	}
	if c.Started <= 0 || end < c.Started || end <= 0 {
		return ""
	}
	seconds := int(math.Round((end - c.Started) / 1000))
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
		return fmt.Sprintf("%.0fk", n/1000)
	}
	return fmt.Sprintf("%.0f", n)
}
func (r *Renderer) WorkerRuntime(c WorkerCard, now float64) string {
	var parts []string
	if c.Preset != "" {
		parts = append(parts, c.Preset)
	}
	if n := workerNumber(c.Usage, "turns"); n > 0 {
		parts = append(parts, fmt.Sprintf("%.0f turns", n))
	}
	var usage []string
	if n := workerNumber(c.Usage, "input"); n > 0 {
		usage = append(usage, "↑"+workerTokens(n))
	}
	if n := workerNumber(c.Usage, "output"); n > 0 {
		usage = append(usage, "↓"+workerTokens(n))
	}
	if len(usage) > 0 {
		parts = append(parts, strings.Join(usage, " "))
	}
	if duration := WorkerDuration(c, now); duration != "" {
		parts = append(parts, duration)
	}
	return r.OneLine(strings.Join(parts, " · "))
}

func WorkerTaskID(item session.Item, index int, position int) string {
	// Native indices are stable; position disambiguates malformed duplicate indices.
	return fmt.Sprintf("worker-task:%s:%d:%d", item.ID, index, position)
}

// Unknown/legacy phases remain visible; display text is never a protocol.
func WorkerSlotNotice(code string) bool {
	return code == "waiting_slot" || code == "slot_acquired"
}

func (r *Renderer) WorkerDetailRows(item session.Item, theme config.Theme, _ Options, width int) ([]string, []Block) {
	width = max(1, width)
	v := r.WorkerData(item)
	var rows []string
	var blocks []Block
	var separators []int
	line := func(s, color string) { rows = append(rows, r.Wrap(r.Paint(r.Clean(s), color), width)...) }
	for _, err := range v.Errors {
		line("Control error: "+err, theme.Error)
	}
	for i, c := range v.Cards {
		if i > 0 {
			separators = append(separators, len(rows))
			rows = append(rows, "")
		}
		start := len(rows)
		icon, color := workerStatus(c.Status, theme)
		header := r.OneLine(workerFirst(c.Mode, "task"))
		if info := r.WorkerRuntime(c, v.Now); info != "" {
			header += " · " + info
		}
		rows = append(rows, r.Fit(r.Paint(icon, color)+" "+r.Paint(header, theme.Text), width))
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
				for _, row := range r.Wrap(r.Paint(r.Clean(text), theme.Text), width-prefixWidth) {
					prefix := ""
					if prefixWidth > 0 {
						prefix = strings.Repeat(" ", prefixWidth)
						if first && marker != "" {
							prefix = "  " + r.Paint(marker, shade) + " "
						}
					}
					rows = append(rows, prefix+row)
					first = false
				}
			}
		}
		if description := workerFirst(strings.TrimSpace(r.Clean(c.Title)), c.Objective); description != "" {
			section("◦", theme.Accent, []string{description})
		}
		steps := r.WorkerToolRows(c, theme, width)
		rows = append(rows, steps...)
		var conclusion []string
		workerResult(map[string]any{"summary": c.Result["summary"]}, func(s, _ string) {
			if s != "结果:" {
				conclusion = append(conclusion, s)
			}
		}, theme)
		if len(conclusion) > 0 {
			section("", theme.Text, conclusion)
		} else {
			// Tool and thinking activities already appear in the steps list.
			var progress []string
			for _, value := range c.Activities {
				a := workerMap(value)
				kind := r.String(a, "type")
				if kind != "text" && kind != "message" && kind != "phase" {
					continue
				}
				text := r.String(a, "detail")
				if kind == "phase" {
					label := r.String(a, "label")
					if r.String(a, "status") != "failed" && WorkerSlotNotice(r.String(a, "phaseCode")) {
						continue
					}
					text = workerFirst(text, label)
				}
				if strings.TrimSpace(text) != "" {
					progress = append(progress, text)
				}
			}
			if len(progress) == 0 && len(steps) == 0 && !WorkerSlotNotice(c.PhaseCode) {
				switch c.Status {
				case "running", "cancelling":
					progress = append(progress, workerFirst(c.Phase, "正在执行中..."))
				case "queued":
					progress = append(progress, workerFirst(c.Phase, "等待执行..."))
				}
			}
			workerResult(map[string]any{"error": c.Result["error"]}, func(s, _ string) {
				if s != "Error" {
					progress = append(progress, s)
				}
			}, theme)
			section("", theme.Muted, progress)
		}
		blocks = append(blocks, Block{ID: WorkerTaskID(item, c.Index, i), Role: "workerTask", MessageIndex: item.MessageIndex, Start: start, End: len(rows), Expanded: true})
	}
	if len(v.Cards) == 0 || v.Result["summary"] != nil || v.Result["error"] != nil || v.Result["validation_errors"] != nil {
		workerResult(v.Result, line, theme)
	}
	if !v.Authoritative && v.Text != "" {
		line(v.Text, theme.Text)
	}
	if len(rows) == 0 {
		line("No task details available", theme.Muted)
	}
	// Size separators from real content, so they never stretch the card.
	contentWidth := 1
	for _, row := range rows {
		contentWidth = max(contentWidth, ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " \t")))
	}
	corners, _ := frames.Palette(theme.FramePreset, "worker")
	c := corners[0]
	separatorColor := fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
	for _, index := range separators {
		rows[index] = r.Paint(strings.Repeat("─", min(width, contentWidth)), separatorColor)
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
func (r *Renderer) WorkerLayout(item session.Item, theme config.Theme, opts Options, width int) ([]string, []Block) {
	width = max(1, width)
	expanded := r.Expanded(item.ID, opts)
	v := r.WorkerData(item)
	rows := []string{r.WorkerHeader(item, v, expanded, theme, width)}
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
	detail, children := r.WorkerDetailRows(item, theme, opts, contentWidth)
	if width >= 8 {
		title := "worker"
		var cardWidth int
		detail, cardWidth = r.Compact(detail, title, width)
		detail = r.Frame(detail, title, cardWidth, "worker", false, width, theme)
	}
	rows = append(rows, detail...)
	blocks[0].End = len(rows)
	tasks := detailview.WorkerTasks(item)
	for i, b := range children {
		if i < len(tasks) && tasks[i].Key != "" {
			b.InspectorParent, b.InspectorTask = item.ID, tasks[i].Key
		}
		b.Start += offset
		b.End += offset
		blocks = append(blocks, b)
	}
	return rows, blocks
}
