package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type workerPresetSettings struct {
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
}
type workerRoutingSettings struct {
	Version              int                  `json:"version"`
	Fast                 workerPresetSettings `json:"fast"`
	Normal               workerPresetSettings `json:"normal"`
	Deep                 workerPresetSettings `json:"deep"`
	MaxConcurrentWorkers int                  `json:"maxConcurrentWorkers"`
	DefaultTimeoutMs     int                  `json:"defaultTimeoutMs"`
	MaxOutputBytes       int                  `json:"maxOutputBytes"`
}
type workerSettingsError struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
	Message  string `json:"message"`
}
type workerModelOption struct {
	ID       string   `json:"id"`
	Thinking []string `json:"thinking"`
}
type workerSettingsRequest struct {
	Config   workerRoutingSettings  `json:"config"`
	Original *workerRoutingSettings `json:"original,omitempty"`
	Models   []workerModelOption    `json:"models"`
	Error    *workerSettingsError   `json:"error,omitempty"`
}
type workerSettingsDialog struct {
	draft, original     workerRoutingSettings
	models              []workerModelOption
	page, preset, field string
}

func (s *workerSettingsDialog) setting() *workerPresetSettings {
	switch s.preset {
	case "fast":
		return &s.draft.Fast
	case "normal":
		return &s.draft.Normal
	default:
		return &s.draft.Deep
	}
}
func workerSettingsErrorText(e *workerSettingsError) string {
	if e == nil {
		return ""
	}
	switch e.Code {
	case "invalid-number":
		return "请输入有效范围内的整数：" + e.Field
	case "model-unavailable":
		return e.Field + " 模型当前不可用：" + e.Model
	case "thinking-unsupported":
		return e.Field + " 模型不支持 Thinking " + e.Thinking
	case "save-failed":
		return "保存失败：" + e.Message
	default:
		return "设置数据无效，请检查后重新保存。"
	}
}
func (m *Model) openWorkerSettings(d *dialog, payload string) bool {
	var request workerSettingsRequest
	if json.Unmarshal([]byte(payload), &request) != nil || request.Config.Version != 2 {
		return false
	}
	original := request.Config
	if request.Original != nil {
		original = *request.Original
	}
	d.workerSettings = &workerSettingsDialog{draft: request.Config, original: original, models: request.Models, page: "main"}
	m.refreshWorkerSettings(d)
	d.message = workerSettingsErrorText(request.Error)
	return true
}

func (m *Model) refreshWorkerSettings(d *dialog) {
	s := d.workerSettings
	d.kind, d.message, d.options, d.selected = "select", "", nil, 0
	d.editor.SetValue("")
	d.editor.Area.Placeholder = "搜索选项"
	add := func(label, value string) { d.options = append(d.options, option{Label: label, Value: value}) }
	switch s.page {
	case "main":
		d.title = "Worker 设置"
		if s.draft != s.original {
			d.title += " · 未保存"
		}
		for _, preset := range []struct {
			name  string
			value workerPresetSettings
		}{{"fast", s.draft.Fast}, {"normal", s.draft.Normal}, {"deep", s.draft.Deep}} {
			add(fmt.Sprintf("%s · %s · %s", preset.name, preset.value.Model, preset.value.Thinking), preset.name)
		}
		add(fmt.Sprintf("并发上限 · %d", s.draft.MaxConcurrentWorkers), "maxConcurrentWorkers")
		add(fmt.Sprintf("默认超时 · %g 秒", float64(s.draft.DefaultTimeoutMs)/1000), "defaultTimeoutMs")
		add(fmt.Sprintf("最大输出 · %g KiB", float64(s.draft.MaxOutputBytes)/1024), "maxOutputBytes")
		add("保存并退出", "save")
		add("取消", "cancel")
	case "preset":
		d.title = s.preset + " 设置"
		add("模型 · "+s.setting().Model, "model")
		add("Thinking · "+s.setting().Thinking, "thinking")
		add("返回", "back")
	case "model":
		d.title = s.preset + " 模型"
		for _, model := range s.models {
			label := model.ID
			if model.ID == s.setting().Model {
				label += " · 当前"
			}
			add(label, model.ID)
		}
		if len(s.models) == 0 {
			d.message = "没有可用模型。Esc 返回。"
		}
	case "thinking":
		d.title = s.preset + " Thinking"
		for _, model := range s.models {
			if model.ID != s.setting().Model {
				continue
			}
			for _, level := range model.Thinking {
				label := level
				if level == s.setting().Thinking {
					label += " · 当前"
				}
				add(label, level)
			}
			break
		}
		if len(d.options) == 0 {
			d.message = "当前模型不可用或没有支持的 Thinking 等级。Esc 返回。"
		}
	case "number":
		d.kind = "input"
		value, min, max, title := s.number()
		d.title = title
		d.message = fmt.Sprintf("请输入 %d–%d 之间的整数", min, max)
		d.editor.SetValue(strconv.FormatFloat(value, 'f', -1, 64))
		d.editor.Area.Placeholder = d.message
	case "discard":
		d.kind, d.title, d.message = "confirm", "放弃更改？", "当前修改尚未保存。"
		add("继续编辑", "no")
		add("放弃更改", "yes")
	}
	m.resizeDialog(d)
}
func (s *workerSettingsDialog) number() (float64, int, int, string) {
	switch s.field {
	case "maxConcurrentWorkers":
		return float64(s.draft.MaxConcurrentWorkers), 1, 16, "并发上限"
	case "defaultTimeoutMs":
		return float64(s.draft.DefaultTimeoutMs) / 1000, 1, 3600, "默认超时（秒）"
	default:
		return float64(s.draft.MaxOutputBytes) / 1024, 8, 1024, "最大输出（KiB）"
	}
}
func (m *Model) cancelWorkerSettings(t *tab, d *dialog) tea.Cmd {
	s := d.workerSettings
	switch s.page {
	case "main":
		if s.draft == s.original {
			return m.finishWorkerSettings(t, d, false)
		}
		s.page = "discard"
	case "model", "thinking":
		s.page = "preset"
	default:
		s.page = "main"
	}
	m.refreshWorkerSettings(d)
	return nil
}
func (m *Model) finishWorkerSettings(t *tab, d *dialog, save bool) tea.Cmd {
	t.dialogs = t.dialogs[1:]
	payload := map[string]any{"cancelled": true}
	if save {
		data, err := json.Marshal(d.workerSettings.draft)
		if err != nil {
			return nil
		}
		payload = map[string]any{"value": string(data)}
	}
	return tea.Batch(respond(t, d, payload), m.focus(t))
}
func (m *Model) submitWorkerSettings(t *tab, d *dialog, value string) tea.Cmd {
	s := d.workerSettings
	switch s.page {
	case "main":
		switch value {
		case "fast", "normal", "deep":
			s.page, s.preset = "preset", value
		case "maxConcurrentWorkers", "defaultTimeoutMs", "maxOutputBytes":
			s.page, s.field = "number", value
		case "save":
			return m.finishWorkerSettings(t, d, true)
		case "cancel":
			return m.cancelWorkerSettings(t, d)
		}
	case "preset":
		switch value {
		case "model", "thinking":
			s.page = value
		case "back":
			s.page = "main"
		}
	case "model":
		s.setting().Model = value
		s.page = "preset"
		m.refreshWorkerSettings(d)
		for _, model := range s.models {
			if model.ID != value {
				continue
			}
			for _, thinking := range model.Thinking {
				if thinking == s.setting().Thinking {
					return nil
				}
			}
		}
		d.message = "模型不支持当前 Thinking；已保留原值，请显式选择支持的等级。"
		return nil
	case "thinking":
		s.setting().Thinking, s.page = value, "preset"
	case "number":
		_, min, max, _ := s.number()
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < min || number > max {
			d.message = fmt.Sprintf("请输入 %d–%d 之间的整数", min, max)
			return nil
		}
		switch s.field {
		case "maxConcurrentWorkers":
			s.draft.MaxConcurrentWorkers = number
		case "defaultTimeoutMs":
			s.draft.DefaultTimeoutMs = number * 1000
		case "maxOutputBytes":
			s.draft.MaxOutputBytes = number * 1024
		}
		s.page = "main"
	case "discard":
		if value == "yes" {
			return m.finishWorkerSettings(t, d, false)
		}
		s.page = "main"
	}
	m.refreshWorkerSettings(d)
	return nil
}
