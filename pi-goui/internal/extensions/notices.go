// Package extensions contains optional third-party UI protocol adapters.
// It is not part of Pi RPC and must never be required for ordinary sessions.
package extensions

import (
	"encoding/json"
	"fmt"
	"strings"
)

const EventPrefix = "pi-goui:event:v1:"

type Event struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data"`
}

// DecodeEvent distinguishes extension traffic from ordinary Pi notifications.
func DecodeEvent(message string) (Event, bool) {
	if !strings.HasPrefix(message, EventPrefix) {
		return Event{}, false
	}
	var event Event
	_ = json.Unmarshal([]byte(strings.TrimPrefix(message, EventPrefix)), &event)
	return event, true
}

// Notice formats known third-party facts. Unknown facts remain ignorable.
func Notice(event Event) string {
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
		return ""
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
	return text
}
