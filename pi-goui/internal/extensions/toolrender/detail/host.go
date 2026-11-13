// Package detail contains optional third-party full tool-result views.
package detail

import (
	"pi-goui/internal/extensionapi"
	"pi-goui/internal/render/tooldata"
	"pi-goui/internal/session"
)

type Renderer struct{ extensionapi.DetailHost }

// Render handles known third-party result views; false preserves generic fallback.
func (v *Renderer) Render(name string, item session.Item, args, details map[string]any) bool {
	switch name {
	case "worker":
		v.Worker(item, args, details)
	case "ask_question":
		v.AskQuestion(item, args, details)
	case "web_search":
		v.WebSearch(item, args, details)
	case "fetch_content":
		v.FetchContent(item, args, details)
	case "source_check":
		v.SourceCheck(item, args, details)
	case "get_search_content":
		v.GetSearchContent(item, args, details)
	case "update_plan":
		v.UpdatePlan(item, args, details)
	case "web_enable":
		v.WebEnable(item, args, details)
	case "obs_recall":
		v.ObsRecall(item, args, details)
	default:
		return false
	}
	return true
}

func New(host extensionapi.DetailHost) *Renderer { return &Renderer{host} }

var copyMap = tooldata.CopyMap
var decodeJSON = tooldata.DecodeJSON
var decodeMap = tooldata.DecodeMap
var stringField = tooldata.StringField
var intField = tooldata.IntField
var toolName = tooldata.ToolName
