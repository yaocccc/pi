package session

import (
	"encoding/json"
	"fmt"
)

// Cached blocks contain only record-local data. Ordinals and run state are
// applied on every projection; live tool overlays never mutate these blocks.
type projectedBlock struct {
	item               Item
	busy, active, tool bool
}
type recordProjection struct {
	visible bool
	blocks  []projectedBlock
}

func (r *record) project() *recordProjection {
	if r.projection != nil {
		return r.projection
	}
	p := &recordProjection{}
	r.projection = p
	m := r.msg
	if r.summary == "" && (m.Role == "" || (m.Role == "custom" && m.Display != nil && !*m.Display)) {
		return p
	}
	p.visible = true
	// Background fetch completion is useful to the agent, not the transcript.
	// Keep the record and its ordinal; suppress only its display blocks.
	if r.summary == "" && m.Role == "custom" && m.CustomType == "web-search-content-ready" {
		return p
	}
	add := func(t Item, busy, active bool) {
		p.blocks = append(p.blocks, projectedBlock{item: t, busy: busy, active: active})
	}
	addTool := func(t Item, busy bool) {
		estimateItem(&t)
		p.blocks = append(p.blocks, projectedBlock{item: t, busy: busy, tool: true})
	}
	if r.summary != "" {
		add(Item{ID: r.id, Role: "system", Text: r.summary}, false, false)
		return p
	}
	if m.Role == "user" {
		text, images := userContent(m.Content)
		if text != "" {
			add(Item{ID: r.id + "/0", Role: "user", Text: text, Images: images, Error: m.StopReason == "error"}, false, false)
		}
		return p
	}
	if m.Role == "toolResult" {
		addTool(Item{ID: "tool:" + m.ToolCallID, Role: "tool", ToolID: m.ToolCallID, ToolName: m.ToolName, Text: textOf(m.Content), Details: m.Details, HasResult: true, Running: m.partial, final: !m.partial, resultChars: resultChars(m.Content), contentKnown: len(m.Content) > 0, Error: m.IsError != nil && *m.IsError, errorKnown: m.IsError != nil}, false)
		return p
	}
	if m.Role == "bashExecution" {
		t := Item{ID: r.id, Role: "tool", ToolID: r.id, ToolName: "bash", Text: m.Output, Details: m.Details, HasResult: true, final: true, bashExecution: true, contentKnown: true, resultChars: utf16Length(m.Output), Error: m.ExitCode != nil && *m.ExitCode != 0, errorKnown: m.ExitCode != nil}
		if len(t.Details) == 0 && m.ExitCode != nil {
			t.Details, _ = json.Marshal(map[string]int{"exitCode": *m.ExitCode})
		}
		t.Arguments, _ = json.Marshal(map[string]string{"command": m.Command})
		addTool(t, false)
		return p
	}
	if m.Role == "compactionSummary" || m.Role == "branchSummary" {
		add(Item{ID: r.id, Role: "system", Text: m.Summary}, false, false)
		return p
	}
	for i, c := range blocks(m.Content) {
		id := fmt.Sprintf("%s/%d", r.id, i)
		switch c.Type {
		case "toolCall":
			args := c.Arguments
			if c.Data != "" && c.Name == "write" {
				// A start snapshot may contain {} while deltas carry the live
				// arguments. toolcall_end replaces the block authoritatively.
				if preview := partialWriteArguments(c.Data); len(preview) > 0 {
					args = preview
				}
			} else if len(args) == 0 && json.Valid([]byte(c.Data)) {
				args = json.RawMessage(c.Data)
			}
			addTool(Item{ID: "tool:" + c.ID, Role: "tool", ToolID: c.ID, ToolName: c.Name, Arguments: args}, true)
		case "thinking":
			add(Item{ID: id, Role: m.Role, Thinking: c.Thinking}, false, true)
		case "text":
			if c.Text != "" {
				add(Item{ID: id, Role: m.Role, Text: c.Text, Error: m.StopReason == "error"}, false, true)
			}
		case "image":
			add(Item{ID: id, Role: m.Role, Text: "[image]"}, false, false)
		}
	}
	if m.ErrorMessage != "" {
		add(Item{ID: r.id + "/error", Role: "assistant", Text: m.ErrorMessage, Error: true}, false, false)
	}
	return p
}
