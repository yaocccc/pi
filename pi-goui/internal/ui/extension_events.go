package ui

import (
	"encoding/json"
	"fmt"
	"strings"
)

const extensionEventPrefix = "pi-goui:event:v1:"

// extensionEvent consumes facts, never extension-supplied styles or layouts.
// Unknown/malformed protocol messages are not shown as raw JSON notifications.
func (t *tab) extensionEvent(message string) bool {
	if !strings.HasPrefix(message, extensionEventPrefix) {
		return false
	}
	var event struct {
		Name string          `json:"name"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(message, extensionEventPrefix)), &event) != nil {
		return true
	}
	var data struct {
		Name          string  `json:"name"`
		Path          string  `json:"path"`
		ContextTokens float64 `json:"contextTokens"`
		DurationMS    float64 `json:"durationMs"`
		GenerationMS  float64 `json:"generationMs"`
		BeforeTokens  float64 `json:"beforeTokens"`
		AfterTokens   float64 `json:"afterTokens"`
		Reason        string  `json:"reason"`
		Level         string  `json:"level"`
		Message       string  `json:"message"`
		Code          string  `json:"code"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return true
	}
	var text string
	switch event.Name {
	case "autoname.renamed":
		text = fmt.Sprintf("会话已自动命名：%s [%.0f tokens %.1fs]", data.Name, data.ContextTokens, data.DurationMS/1000)
	case "filter.redacted":
		text = "已隐藏敏感文件内容：" + data.Path
	case "context.ready":
		text = fmt.Sprintf("上下文摘要已就绪，生成耗时 %.1fs；将在下一个安全边界应用。", data.GenerationMS/1000)
	case "context.applied":
		change := "不变"
		if delta := data.BeforeTokens - data.AfterTokens; delta != 0 {
			direction := "减少"
			if delta < 0 {
				direction, delta = "增加", -delta
			}
			change = fmt.Sprintf("%s %.0f", direction, delta)
			if data.BeforeTokens > 0 {
				change += fmt.Sprintf("，%.1f%%", delta/data.BeforeTokens*100)
			}
		}
		text = fmt.Sprintf("上下文已压缩：约 %.0f → %.0f tokens（%s）；生成 %.1fs，总耗时 %.1fs（含等待安全边界）。新增消息已保留。", data.BeforeTokens, data.AfterTokens, change, data.GenerationMS/1000, data.DurationMS/1000)
	case "context.failed":
		text = fmt.Sprintf("后台上下文压缩未完成（耗时 %.1fs），当前对话不受影响。原因：%s", data.DurationMS/1000, data.Reason)
	case "worker.settings-saved":
		text = "Worker 设置已保存，后续任务立即生效"
	case "worker.settings-error":
		text = "Worker 设置保存失败：" + data.Message
	case "codex.settings-saved":
		text = "Codex 档位已设为 " + data.Level
	case "codex.settings-error":
		if data.Code == "invalid-level" {
			text = "Codex 设置失败：无效的档位"
		} else {
			text = "保存 Fast 配置失败：" + data.Message
		}
	}
	if text != "" {
		t.notify(text)
	}
	return true
}
