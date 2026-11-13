package hostui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/extensions/worker"
)

type workerPresetSettings = worker.PresetSettings
type workerRoutingSettings = worker.RoutingSettings
type workerSettingsError = worker.SettingsError
type workerModelOption = worker.ModelOption
type workerSettingsRequest = worker.SettingsRequest
type WorkerSettings struct {
	Draft, Original     workerRoutingSettings
	Models              []workerModelOption
	Page, Preset, Field string
}

func (s *WorkerSettings) setting() *workerPresetSettings {
	switch s.Preset {
	case "fast":
		return &s.Draft.Fast
	case "normal":
		return &s.Draft.Normal
	default:
		return &s.Draft.Deep
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
func (h DialogHost) OpenWorkerSettings(d *Dialog, payload string) bool {
	var request workerSettingsRequest
	if json.Unmarshal([]byte(payload), &request) != nil || request.Config.Version != 2 {
		return false
	}
	original := request.Config
	if request.Original != nil {
		original = *request.Original
	}
	d.WorkerSettings = &WorkerSettings{Draft: request.Config, Original: original, Models: request.Models, Page: "main"}
	h.RefreshWorkerSettings(d)
	d.Message = workerSettingsErrorText(request.Error)
	return true
}

func (h DialogHost) RefreshWorkerSettings(d *Dialog) {
	s := d.WorkerSettings
	d.Kind, d.Message, d.Options, d.Selected = "select", "", nil, 0
	d.Editor.SetValue("")
	d.Editor.Area.Placeholder = "搜索选项"
	add := func(label, value string) { d.Options = append(d.Options, Option{Label: label, Value: value}) }
	switch s.Page {
	case "main":
		d.Title = "Worker 设置"
		if s.Draft != s.Original {
			d.Title += " · 未保存"
		}
		for _, preset := range []struct {
			name  string
			value workerPresetSettings
		}{{"fast", s.Draft.Fast}, {"normal", s.Draft.Normal}, {"deep", s.Draft.Deep}} {
			add(fmt.Sprintf("%s · %s · %s", preset.name, preset.value.Model, preset.value.Thinking), preset.name)
		}
		add(fmt.Sprintf("并发上限 · %d", s.Draft.MaxConcurrentWorkers), "maxConcurrentWorkers")
		add(fmt.Sprintf("默认超时 · %g 秒", float64(s.Draft.DefaultTimeoutMs)/1000), "defaultTimeoutMs")
		add(fmt.Sprintf("最大输出 · %g KiB", float64(s.Draft.MaxOutputBytes)/1024), "maxOutputBytes")
		add("保存并退出", "save")
		add("取消", "cancel")
	case "preset":
		d.Title = s.Preset + " 设置"
		add("模型 · "+s.setting().Model, "model")
		add("Thinking · "+s.setting().Thinking, "thinking")
		add("返回", "back")
	case "model":
		d.Title = s.Preset + " 模型"
		for _, model := range s.Models {
			label := model.ID
			if model.ID == s.setting().Model {
				label += " · 当前"
			}
			add(label, model.ID)
		}
		if len(s.Models) == 0 {
			d.Message = "没有可用模型。Esc 返回。"
		}
	case "thinking":
		d.Title = s.Preset + " Thinking"
		for _, model := range s.Models {
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
		if len(d.Options) == 0 {
			d.Message = "当前模型不可用或没有支持的 Thinking 等级。Esc 返回。"
		}
	case "number":
		d.Kind = "input"
		value, min, max, title := s.number()
		d.Title = title
		d.Message = fmt.Sprintf("请输入 %d–%d 之间的整数", min, max)
		d.Editor.SetValue(strconv.FormatFloat(value, 'f', -1, 64))
		d.Editor.Area.Placeholder = d.Message
	case "discard":
		d.Kind, d.Title, d.Message = "confirm", "放弃更改？", "当前修改尚未保存。"
		add("继续编辑", "no")
		add("放弃更改", "yes")
	}
	h.Resize()
}
func (s *WorkerSettings) number() (float64, int, int, string) {
	switch s.Field {
	case "maxConcurrentWorkers":
		return float64(s.Draft.MaxConcurrentWorkers), 1, 16, "并发上限"
	case "defaultTimeoutMs":
		return float64(s.Draft.DefaultTimeoutMs) / 1000, 1, 3600, "默认超时（秒）"
	default:
		return float64(s.Draft.MaxOutputBytes) / 1024, 8, 1024, "最大输出（KiB）"
	}
}
func (h DialogHost) CancelWorkerSettings(d *Dialog) tea.Cmd {
	s := d.WorkerSettings
	switch s.Page {
	case "main":
		if s.Draft == s.Original {
			return h.FinishWorkerSettings(d, false)
		}
		s.Page = "discard"
	case "model", "thinking":
		s.Page = "preset"
	default:
		s.Page = "main"
	}
	h.RefreshWorkerSettings(d)
	return nil
}
func (h DialogHost) FinishWorkerSettings(d *Dialog, save bool) tea.Cmd {
	payload := map[string]any{"cancelled": true}
	if save {
		data, err := json.Marshal(d.WorkerSettings.Draft)
		if err != nil {
			return nil
		}
		payload = map[string]any{"value": string(data)}
	}
	return h.Close(payload)
}
func (h DialogHost) SubmitWorkerSettings(d *Dialog, value string) tea.Cmd {
	s := d.WorkerSettings
	switch s.Page {
	case "main":
		switch value {
		case "fast", "normal", "deep":
			s.Page, s.Preset = "preset", value
		case "maxConcurrentWorkers", "defaultTimeoutMs", "maxOutputBytes":
			s.Page, s.Field = "number", value
		case "save":
			return h.FinishWorkerSettings(d, true)
		case "cancel":
			return h.CancelWorkerSettings(d)
		}
	case "preset":
		switch value {
		case "model", "thinking":
			s.Page = value
		case "back":
			s.Page = "main"
		}
	case "model":
		s.setting().Model = value
		s.Page = "preset"
		h.RefreshWorkerSettings(d)
		for _, model := range s.Models {
			if model.ID != value {
				continue
			}
			for _, thinking := range model.Thinking {
				if thinking == s.setting().Thinking {
					return nil
				}
			}
		}
		d.Message = "模型不支持当前 Thinking；已保留原值，请显式选择支持的等级。"
		return nil
	case "thinking":
		s.setting().Thinking, s.Page = value, "preset"
	case "number":
		_, min, max, _ := s.number()
		number, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || number < min || number > max {
			d.Message = fmt.Sprintf("请输入 %d–%d 之间的整数", min, max)
			return nil
		}
		switch s.Field {
		case "maxConcurrentWorkers":
			s.Draft.MaxConcurrentWorkers = number
		case "defaultTimeoutMs":
			s.Draft.DefaultTimeoutMs = number * 1000
		case "maxOutputBytes":
			s.Draft.MaxOutputBytes = number * 1024
		}
		s.Page = "main"
	case "discard":
		if value == "yes" {
			return h.FinishWorkerSettings(d, false)
		}
		s.Page = "main"
	}
	h.RefreshWorkerSettings(d)
	return nil
}
