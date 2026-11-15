package session

import "encoding/json"

// Historical metadata is a read-only projection, not backend state. In
// particular, model limits/URLs and live queues cannot be inferred from JSONL.
type previewMetadata struct {
	model    ModelInfo
	thinking string
	stats    Stats
}

type previewUsage struct {
	Input, Output, CacheRead, CacheWrite, TotalTokens int
	Cost                                              struct{ Total *float64 }
}

// Count native tool calls without retaining/decoding text, arguments or images.
// Legacy string content is valid history but contains no structured tool calls.
type previewContent []struct{ Type string }

func (p *previewContent) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	type plain previewContent
	return json.Unmarshal(raw, (*plain)(p))
}

type previewMessage struct {
	Role, Provider, Model, StopReason string
	Content                           previewContent
	Usage                             json.RawMessage
}

type previewAssistant struct {
	model  ModelInfo
	tokens *int
}

func parsePreviewUsage(raw json.RawMessage) *previewUsage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var usage previewUsage
	if json.Unmarshal(raw, &usage) != nil || usage.Input < 0 || usage.Output < 0 || usage.CacheRead < 0 || usage.CacheWrite < 0 || usage.TotalTokens < 0 {
		return nil
	}
	return &usage
}

func addPreviewUsage(stats *Stats, usage *previewUsage) {
	if usage == nil {
		stats.HasCost = false
		return
	}
	stats.Input += usage.Input
	stats.Output += usage.Output
	stats.CacheRead += usage.CacheRead
	stats.CacheWrite += usage.CacheWrite
	if usage.Cost.Total != nil && *usage.Cost.Total >= 0 {
		stats.Cost += *usage.Cost.Total
	} else {
		stats.HasCost = false
	}
}

func previewString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func preparePreviewMetadata(entries []entry, branch *snapshotCache) *previewMetadata {
	out := &previewMetadata{stats: Stats{HasCost: true}}
	assistants := make(map[string]previewAssistant)
	// Native get_session_stats counts all persisted entries, not only the
	// current branch. Each entry's usage contributes exactly once.
	for _, e := range entries {
		switch e.Type {
		case "usage", "compaction", "branch_summary":
			if len(e.Usage) > 0 && string(e.Usage) != "null" {
				addPreviewUsage(&out.stats, parsePreviewUsage(e.Usage))
			}
		case "message":
			out.stats.TotalMessages++
			var m previewMessage
			if json.Unmarshal(e.Message, &m) != nil {
				continue // Optional metadata must not hide readable history.
			}
			switch m.Role {
			case "user":
				out.stats.UserMessages++
			case "toolResult":
				out.stats.ToolResults++
				if len(m.Usage) > 0 && string(m.Usage) != "null" {
					addPreviewUsage(&out.stats, parsePreviewUsage(m.Usage))
				}
			case "assistant":
				out.stats.AssistantMessages++
				for _, c := range m.Content {
					if c.Type == "toolCall" {
						out.stats.ToolCalls++
					}
				}
				usage := parsePreviewUsage(m.Usage)
				addPreviewUsage(&out.stats, usage)
				if branch.pathIDs[e.ID] {
					a := previewAssistant{model: ModelInfo{ID: m.Model, Provider: m.Provider}}
					if usage != nil && m.StopReason != "aborted" && m.StopReason != "error" {
						tokens := usage.TotalTokens
						if tokens == 0 {
							tokens = usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
						}
						if tokens > 0 {
							a.tokens = &tokens
						}
					}
					assistants[e.ID] = a
				}
			}
		}
	}
	out.stats.Total = out.stats.Input + out.stats.Output + out.stats.CacheRead + out.stats.CacheWrite

	var fallback ModelInfo
	modelChanged := false
	// The parsed branch is leaf-to-root. Replay chronologically so newer
	// settings win, and unrelated leaves cannot change the selected model.
	for i := len(branch.path) - 1; i >= 0; i-- {
		e := branch.path[i]
		switch e.Type {
		case "model_change":
			modelChanged = true
			out.model = ModelInfo{ID: previewString(e.ModelID), Provider: previewString(e.Provider)}
		case "thinking_level_change":
			out.thinking = previewString(e.ThinkingLevel)
		case "message":
			// A usage snapshot is reliable only with no subsequent context
			// messages. Estimating trailing user/tool text belongs to native Pi.
			out.stats.ContextTokens = nil
			if assistant, ok := assistants[e.ID]; ok {
				if assistant.model.ID != "" && assistant.model.Provider != "" {
					fallback = assistant.model
				}
				out.stats.ContextTokens = assistant.tokens
			}
		case "compaction", "branch_summary", "context_edit", "custom_message":
			// Never reuse usage measured before a compaction/context rewrite.
			out.stats.ContextTokens = nil
		}
	}
	if !modelChanged {
		out.model = fallback
	}
	return out
}
