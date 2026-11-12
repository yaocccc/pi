package ui

import (
	"encoding/json"
	"io"
	"strings"

	"pi-goui/internal/session"
)

// Presentation only: neither replay state nor the immutable visible snapshot is
// rewritten. Called on document invalidation, never on scrolling or hovering.
func workerReportItems(items []session.Item) []session.Item {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "assistant" || strings.TrimSpace(items[i].Text) == "" {
			continue
		}
		formatted := workerReportMarkdown(items[i].Text)
		if formatted == items[i].Text {
			return items
		}
		copyItems := append([]session.Item(nil), items...)
		copyItems[i].Text = formatted
		return copyItems
	}
	return items
}

func workerReportMarkdown(original string) string {
	source := strings.TrimSpace(original)
	if strings.HasPrefix(source, "```json\n") && strings.HasSuffix(source, "\n```") {
		source = strings.TrimSuffix(strings.TrimPrefix(source, "```json\n"), "\n```")
	}
	decoder := json.NewDecoder(strings.NewReader(source))
	decoder.UseNumber()
	var report map[string]any
	if decoder.Decode(&report) != nil || report == nil {
		return original
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return original
	}
	status, ok := report["status"].(string)
	if !ok || strings.TrimSpace(status) == "" {
		return original
	}
	recognized := false
	for _, key := range []string{"summary", "validation", "acceptance"} {
		switch report[key].(type) {
		case string, []any:
			recognized = true
		}
	}
	if !recognized {
		return original
	}
	var out strings.Builder
	out.WriteString("**状态：** " + status + "\n")
	delete(report, "status")
	sections := [][2]string{{"summary", "摘要"}, {"changed_files", "改动文件"}, {"validation", "验证"}, {"acceptance", "验收"}, {"findings", "发现"}, {"risks", "风险"}, {"out_of_scope", "范围外事项"}, {"recommended_next_action", "后续建议"}}
	for _, section := range sections {
		value, exists := report[section[0]]
		if !exists {
			continue
		}
		delete(report, section[0])
		if values, ok := value.([]any); ok && len(values) == 0 {
			continue
		}
		out.WriteString("\n## " + section[1] + "\n\n")
		workerReportValue(&out, value, section[0])
	}
	if len(report) > 0 {
		out.WriteString("\n## 其他字段\n\n")
		workerReportJSON(&out, report)
	}
	return strings.TrimSpace(out.String())
}

func workerReportValue(out *strings.Builder, value any, section string) {
	switch v := value.(type) {
	case string:
		out.WriteString(v + "\n")
	case []any:
		for _, entry := range v {
			if text, ok := entry.(string); ok {
				if section == "changed_files" {
					out.WriteString("- " + workerReportInline(text) + "\n")
				} else {
					out.WriteString("- " + strings.ReplaceAll(text, "\n", "\n  ") + "\n")
				}
			} else if fields, ok := entry.(map[string]any); ok && (section == "validation" || section == "acceptance" || section == "findings") {
				workerReportFields(out, fields)
			} else {
				workerReportJSON(out, entry)
			}
		}
	default:
		workerReportJSON(out, value)
	}
}

func workerReportFields(out *strings.Builder, fields map[string]any) {
	labels := [][2]string{{"criterion", "条件"}, {"severity", "严重程度"}, {"result", "结果"}, {"command", "命令"}, {"file", "文件"}, {"location", "位置"}, {"problem", "问题"}, {"details", "详情"}, {"evidence", "证据"}, {"impact", "影响"}, {"recommendation", "建议"}, {"confidence", "置信度"}}
	remaining := make(map[string]any, len(fields))
	for key, value := range fields {
		remaining[key] = value
	}
	for _, label := range labels {
		value, exists := remaining[label[0]]
		if !exists {
			continue
		}
		delete(remaining, label[0])
		if text, ok := value.(string); ok {
			if label[0] == "command" && strings.Contains(text, "\n") {
				out.WriteString("\n**" + label[1] + "：**\n\n")
				workerReportFence(out, "", text)
			} else {
				if label[0] == "command" {
					text = workerReportInline(text)
				}
				out.WriteString("- **" + label[1] + "：** " + strings.ReplaceAll(text, "\n", "\n  ") + "\n")
			}
		} else {
			out.WriteString("\n**" + label[1] + "：**\n\n")
			workerReportJSON(out, value)
		}
	}
	if len(remaining) > 0 {
		workerReportJSON(out, remaining)
	}
	if len(fields) == 0 {
		workerReportJSON(out, fields)
	}
}

func workerReportJSON(out *strings.Builder, value any) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		workerReportFence(out, "json", string(raw))
	}
}

func workerReportFence(out *strings.Builder, language, text string) {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	out.WriteString(fence + language + "\n" + text + "\n" + fence + "\n\n")
}

func workerReportInline(text string) string {
	// Fence runs must not occur inside the path; spaces preserve edge backticks.
	fence := "`"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + " " + text + " " + fence
}
