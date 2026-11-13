package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

type content struct {
	Type, Text, Thinking, ID, Name string
	Arguments                      json.RawMessage
	Data, MimeType                 string
}
type message struct {
	Role                     string          `json:"role"`
	CustomType               string          `json:"customType,omitempty"`
	Content                  json.RawMessage `json:"content"`
	Details                  json.RawMessage `json:"details,omitempty"`
	partial                  bool
	Timestamp                json.RawMessage `json:"timestamp"`
	ToolCallID               string          `json:"toolCallId"`
	ToolName                 string          `json:"toolName"`
	IsError                  *bool           `json:"isError,omitempty"`
	ErrorMessage             string          `json:"errorMessage"`
	StopReason               string          `json:"stopReason"`
	Command, Output, Summary string
	ExitCode                 *int  `json:"exitCode"`
	Display                  *bool `json:"display"`
	// stream is the authoritative content while applying deltas. Its slice and
	// blocks are immutable after publication; the next delta replaces them.
	// Content is nil in this mode, not a second serialized copy of the body.
	stream []content
}
type record struct {
	id, key string
	// Keep backend identity separate from the stable display alias.
	entryID string
	msg     message
	summary string
	// Owned by this record version; replaced together with msg or summary.
	projection *recordProjection
	compaction *CompactionMetrics
}
type entry struct {
	Type, ID      string
	ParentID      *string `json:"parentId"`
	Message       json.RawMessage
	Summary, Name string
	CustomType    string
	Data          json.RawMessage
}

func keyOf(m message) string {
	// Several queued messages can share Date.now(). Include canonical content,
	// not just the timestamp, so distinct messages never overwrite each other.
	m.Content, _ = json.Marshal(m.contentBlocks())
	b, _ := json.Marshal(messageJSON(m))
	return fmt.Sprintf("%s:%x", m.Role, sha256.Sum256(b))
}
func blocks(raw json.RawMessage) []content {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []content{{Type: "text", Text: text}}
	}
	var out []content
	_ = json.Unmarshal(raw, &out)
	return out
}
func textOf(raw json.RawMessage) string {
	var b strings.Builder
	for _, c := range blocks(raw) {
		switch c.Type {
		case "text":
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(c.Text)
		case "image":
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("[image]")
		}
	}
	return b.String()
}
func (s *State) init() {
	if s.tools == nil {
		s.tools = map[string]Item{}
	}
	if s.aliases == nil {
		s.aliases = map[string]string{}
	}
}
func (s *State) LoadState(raw json.RawMessage) error {
	var v struct {
		SessionID, SessionFile, SessionName, Cwd, ThinkingLevel string
		Model                                                   ModelInfo
		IsStreaming, IsCompacting                               bool
		Steering, FollowUp                                      []string
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if s.loadedState && v.SessionID != "" && s.ID != "" && s.ID != v.SessionID {
		s.records = nil
		s.live = nil
		s.tools = nil
		s.toolIDs = nil
		s.aliases = nil
		s.pathIDs = nil
		s.leaf = ""
		s.active = ""
		s.runActive = false
		s.Items = nil
		s.MessageCount = 0
		s.extensionState.Reset()
	}
	s.ID = v.SessionID
	s.File = v.SessionFile
	s.Name = v.SessionName
	if v.Cwd != "" {
		s.Cwd = v.Cwd
	}
	s.Model = v.Model
	s.Thinking = v.ThinkingLevel
	// A get_state response can race agent_start; only settled ends an observed run.
	if v.IsStreaming {
		s.runActive = true
	}
	s.Busy = s.runActive
	s.Compacting = v.IsCompacting || s.Compacting
	s.loadedState = true
	if v.Steering != nil {
		s.Steering = v.Steering
	}
	if v.FollowUp != nil {
		s.FollowUp = v.FollowUp
	}
	return nil
}
func (s *State) LoadStats(raw json.RawMessage) error {
	var v struct {
		Tokens       struct{ Input, Output, CacheRead, CacheWrite, Total, TotalTokens int }
		Cost         *float64
		ContextUsage *struct {
			Tokens        *int
			Percent       *float64
			ContextWindow int
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	s.Stats = Stats{Input: v.Tokens.Input, Output: v.Tokens.Output, CacheRead: v.Tokens.CacheRead, CacheWrite: v.Tokens.CacheWrite, Total: v.Tokens.Total, ContextWindow: s.Model.ContextWindow}
	if s.Stats.Total == 0 {
		s.Stats.Total = v.Tokens.TotalTokens
	}
	if s.Stats.Total == 0 {
		s.Stats.Total = s.Stats.Input + s.Stats.Output + s.Stats.CacheRead + s.Stats.CacheWrite
	}
	if v.Cost != nil {
		s.Stats.Cost = *v.Cost
		s.Stats.HasCost = true
	}
	if v.ContextUsage != nil {
		s.Stats.ContextTokens = v.ContextUsage.Tokens
		s.Stats.ContextPercent = v.ContextUsage.Percent
		s.Stats.ContextWindow = v.ContextUsage.ContextWindow
	}
	return nil
}
func (s *State) LoadEntries(raw json.RawMessage) error {
	prepared, err := s.SnapshotPreparation().Prepare(nil, raw, nil)
	if err != nil {
		return err
	}
	s.ApplySnapshot(prepared)
	return nil
}

// loadEntries is used only on the detached preparation state.
func (s *State) loadEntries(raw json.RawMessage, history *[]entry) error {
	var v struct {
		Entries []entry         `json:"entries"`
		LeafID  json.RawMessage `json:"leafId"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	index := map[string]entry{}
	for _, e := range v.Entries {
		if e.ID != "" {
			if _, exists := index[e.ID]; exists {
				return fmt.Errorf("duplicate session entry %s", e.ID)
			}
			index[e.ID] = e
		}
	}
	leaf := ""
	if len(v.LeafID) == 0 {
		for _, e := range v.Entries {
			if e.Type != "session" && e.ID != "" {
				leaf = e.ID
			}
		}
	} else if string(v.LeafID) != "null" {
		if err := json.Unmarshal(v.LeafID, &leaf); err != nil {
			return err
		}
	}
	var path []entry
	seen := map[string]bool{}
	for id := leaf; id != ""; {
		if seen[id] {
			return fmt.Errorf("cycle in session branch at %s", id)
		}
		seen[id] = true
		e, ok := index[id]
		if !ok {
			return fmt.Errorf("missing session entry %s", id)
		}
		path = append(path, e)
		if e.ParentID == nil {
			break
		}
		id = *e.ParentID
	}
	s.init()
	branchChanged := s.leaf != "" && leaf != s.leaf && !seen[s.leaf]
	priorDetails := map[string]json.RawMessage{}
	if !branchChanged {
		// Deltas mutate the live message after its initial key was recorded.
		// Compare the current snapshot when a refresh races message_end.
		for i := range s.live {
			r := &s.live[i]
			r.key = keyOf(r.msg)
			if s.aliases[r.key] == "" {
				s.aliases[r.key] = r.id
			}
		}
		for _, records := range [][]record{s.records, s.live} {
			for _, r := range records {
				if r.msg.Role == "toolResult" && len(r.msg.Details) > 0 {
					priorDetails[r.msg.ToolCallID] = r.msg.Details
				}
			}
		}
		for id, t := range s.tools {
			if len(priorDetails[id]) == 0 {
				priorDetails[id] = t.Details
			}
		}
	}
	extensionProjection := s.newExtensionSnapshot(path)
	var next []record
	keys := map[string]bool{}
	persistedTools := map[string]bool{}
	usedIDs := map[string]bool{}
	displayIDs := map[string]string{}
	for _, r := range s.records {
		if r.entryID != "" {
			displayIDs[r.entryID] = r.id
		}
	}
	for i := len(path) - 1; i >= 0; i-- {
		e := path[i]
		extensionProjection.observeEntry(e)
		switch e.Type {
		case "message":
			var m message
			if err := json.Unmarshal(e.Message, &m); err != nil {
				return fmt.Errorf("entry %s: %w", e.ID, err)
			}
			if m.Role == "toolResult" {
				persistedTools[m.ToolCallID] = true
				if len(m.Details) == 0 {
					m.Details = priorDetails[m.ToolCallID]
				}
			}
			extensionProjection.observeMessage(m)
			k := keyOf(m)
			id := displayIDs[e.ID]
			if id == "" {
				id = s.aliases[k]
			}
			if id == "" || usedIDs[id] {
				id = e.ID
				s.aliases[k] = id
			}
			usedIDs[id] = true
			keys[k] = true
			next = append(next, record{id: id, entryID: e.ID, key: k, msg: m})
		case "compaction":
			next = append(next, record{id: e.ID, summary: "Context compacted — earlier messages retained\n" + e.Summary, compaction: extensionProjection.compaction(e.ID)})
		case "branch_summary":
			next = append(next, record{id: e.ID, summary: "Branch summary\n" + e.Summary})
		case "session_info":
			s.Name = e.Name
		}
	}
	// An intentional tree navigation can move to an ancestor or sibling. Never
	// leak transient messages/tools from the branch being left.
	if branchChanged {
		s.live = nil
		s.tools = map[string]Item{}
		s.toolIDs = nil
		s.active = ""
	}
	var remaining []record
	for _, r := range s.live {
		if !keys[r.key] && !(r.msg.Role == "toolResult" && persistedTools[r.msg.ToolCallID]) {
			remaining = append(remaining, r)
		}
	}
	s.commitExtensionSnapshot(extensionProjection, branchChanged)
	s.live = remaining
	s.records = next
	s.leaf = leaf
	s.pathIDs = seen
	s.project()
	// Extension snapshots only need metadata. Do not retain another copy of
	// every message body alongside the prepared records.
	for i := range path {
		path[i].Message = nil
	}
	*history = path
	return nil
}

// LoadMessages is only a compatibility fallback. Once full entries are loaded,
// a compacted get_messages response must never erase that history.
func (s *State) LoadMessages(raw json.RawMessage) error {
	var v struct{ Messages []message }
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	s.init()
	known := map[string]bool{}
	for _, r := range s.records {
		known[r.key] = true
	}
	for _, r := range s.live {
		known[r.key] = true
	}
	s.observeExtensionFallback(v.Messages)
	for _, m := range v.Messages {
		k := keyOf(m)
		if known[k] {
			continue
		}
		s.serial++
		id := fmt.Sprintf("message-%d", s.serial)
		s.aliases[k] = id
		s.live = append(s.live, record{id: id, key: k, msg: m})
		known[k] = true
	}
	s.project()
	return nil
}
func (s *State) upsert(m message, finished bool) {
	s.init()
	s.observeExtensionMessage(m, finished)
	if m.Role == "toolResult" {
		m.partial = !finished
	}
	k := keyOf(m)
	for i := range s.live {
		r := &s.live[i]
		if r.key == k || (m.Role == "assistant" && r.id == s.active) || (m.Role == "toolResult" && r.msg.Role == "toolResult" && m.ToolCallID != "" && m.ToolCallID == r.msg.ToolCallID) {
			if m.Role == "toolResult" {
				if !r.msg.partial && !finished {
					return
				}
				if len(m.Details) == 0 {
					m.Details = r.msg.Details
				}
				if len(m.Content) == 0 {
					m.Content = r.msg.Content
				}
				if m.IsError == nil {
					m.IsError = r.msg.IsError
				}
				k = keyOf(m)
			}
			old := r.key
			r.msg = m
			r.projection = nil
			r.key = k
			s.aliases[k] = r.id
			if old != k {
				delete(s.aliases, old)
			}
			if finished && r.id == s.active {
				s.active = ""
			}
			s.project()
			return
		}
	}
	for _, r := range s.records {
		if r.key == k {
			return
		}
	}
	s.serial++
	id := fmt.Sprintf("live-%d", s.serial)
	s.aliases[k] = id
	s.live = append(s.live, record{id: id, key: k, msg: m})
	if m.Role == "assistant" && !finished {
		s.active = id
	}
	s.project()
}
func (s *State) Apply(raw json.RawMessage) bool {
	var e struct {
		Type                  string
		Message               json.RawMessage
		AssistantMessageEvent struct {
			Type                         string
			ContentIndex                 int
			Delta, Content, ID, ToolName string
			ToolCall                     json.RawMessage
			Message, Error               json.RawMessage
		}
		ToolCallID, ToolName        string
		ParentToolCallID            string
		Args, PartialResult, Result json.RawMessage
		IsError                     *bool
		Steering, FollowUp          []string
		Name, Level                 string
		Entry                       json.RawMessage
	}
	if json.Unmarshal(raw, &e) != nil {
		return false
	}
	s.init()
	s.observeExtensionEvent(raw)
	refresh := false
	switch e.Type {
	case "agent_start":
		s.Busy = true
		s.runActive = true
	case "agent_end": // May be followed by retries or queue processing.
	case "agent_settled":
		s.Busy = false
		s.runActive = false
		s.Compacting = false
		s.active = ""
		refresh = true
	case "compaction_start":
		s.Compacting = true
	case "compaction_end":
		s.Compacting = false
		refresh = true
	case "queue_update":
		s.Steering = e.Steering
		s.FollowUp = e.FollowUp
	case "session_info_changed":
		s.Name = e.Name
	case "thinking_level_changed":
		s.Thinking = e.Level
	case "entry_appended":
		refresh = true
	case "message_start", "message_end":
		var m message
		if json.Unmarshal(e.Message, &m) == nil && m.Role != "" {
			s.upsert(m, e.Type == "message_end")
		}
	case "message_update":
		// pi 1.5 emits delta-only updates; older clients may send snapshots.
		if len(e.Message) > 0 && string(e.Message) != "null" {
			var m message
			if json.Unmarshal(e.Message, &m) == nil {
				s.upsert(m, false)
				break
			}
		}
		u := e.AssistantMessageEvent
		if u.Type == "done" || u.Type == "error" {
			r := u.Message
			if u.Type == "error" {
				r = u.Error
			}
			var m message
			if json.Unmarshal(r, &m) == nil && m.Role != "" {
				s.upsert(m, true)
			}
			break
		}
		if u.ContentIndex < 0 || u.ContentIndex > 100000 {
			break
		}
		idx := -1
		for i := range s.live {
			if s.live[i].id == s.active {
				idx = i
				break
			}
		}
		if idx < 0 {
			s.serial++
			id := fmt.Sprintf("live-%d", s.serial)
			s.live = append(s.live, record{id: id, msg: message{Role: "assistant", Content: json.RawMessage("[]")}})
			s.active = id
			idx = len(s.live) - 1
		}
		r := &s.live[idx]
		b := r.msg.contentBlocks()
		if r.msg.stream != nil {
			// Checkpoints and background preparations may still hold this
			// message version. Never mutate their blocks or slice backing array.
			b = append([]content(nil), b...)
		}
		for len(b) <= u.ContentIndex {
			b = append(b, content{})
		}
		c := &b[u.ContentIndex]
		switch u.Type {
		case "text_start":
			c.Type = "text"
		case "text_delta":
			c.Type = "text"
			c.Text += u.Delta
		case "text_end":
			c.Type = "text"
			c.Text = u.Content
		case "thinking_start":
			c.Type = "thinking"
		case "thinking_delta":
			c.Type = "thinking"
			c.Thinking += u.Delta
		case "thinking_end":
			c.Type = "thinking"
			c.Thinking = u.Content
		case "toolcall_start":
			c.Type = "toolCall"
			c.ID = u.ID
			c.Name = u.ToolName
		case "toolcall_delta":
			c.Type = "toolCall"
			c.Data += u.Delta
		case "toolcall_end":
			var final content
			if json.Unmarshal(u.ToolCall, &final) == nil {
				*c = final
			}
		}
		for i := range b {
			if b[i].Arguments == nil {
				// The previous JSON round-trip materialized absent raw
				// arguments as null; preserve that projection distinction.
				b[i].Arguments = json.RawMessage("null")
			}
		}
		r.msg.Content, r.msg.stream = nil, b
		r.projection = nil
		s.project()
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		// Nested calls belong to their parent's result/details, not the
		// transcript. Caching them here leaves orphan tools at the end of
		// every subsequent turn because no top-level message references them.
		if e.ParentToolCallID != "" {
			break
		}
		t, exists := s.tools[e.ToolCallID]
		if t.final && e.Type != "tool_execution_end" {
			break
		}
		if !exists {
			s.toolIDs = append(s.toolIDs, e.ToolCallID)
		}
		t.ID = "tool:" + e.ToolCallID
		t.Role = "tool"
		t.ToolID = e.ToolCallID
		if e.ToolName != "" {
			t.ToolName = e.ToolName
		}
		if len(e.Args) > 0 {
			t.Arguments = e.Args
			t.argumentCharsKnown = false
		}
		t.Running = e.Type != "tool_execution_end"
		t.final = !t.Running
		result := e.PartialResult
		if t.final {
			result = e.Result
			t.HasResult = true
		}
		if len(result) > 0 && string(result) != "null" {
			var res struct {
				Content, Details json.RawMessage
				IsError          *bool
			}
			if json.Unmarshal(result, &res) == nil {
				t.setResult(res.Content, res.Details, res.IsError)
			}
		}
		if e.IsError != nil {
			t.Error = *e.IsError
			t.errorKnown = true
		}
		estimateItem(&t)
		s.tools[e.ToolCallID] = t
		s.project()
	}
	return refresh
}
func (s *State) project() {
	var items []Item
	messageIndex := 0
	toolIndex := map[string]int{}
	appendTool := func(t Item) {
		t.MessageIndex = messageIndex
		if idx, ok := toolIndex[t.ToolID]; ok {
			items[idx] = mergeTool(t, items[idx])
		} else {
			toolIndex[t.ToolID] = len(items)
			items = append(items, t)
		}
	}
	for _, records := range [][]record{s.records, s.live} {
		for i := range records {
			r := &records[i]
			p := r.project()
			if !p.visible {
				continue
			}
			messageIndex++
			// Optional visibility does not change native ordinals or caches.
			if r.summary == "" && s.hideExtensionMessage(r.msg) {
				continue
			}
			for _, block := range p.blocks {
				t := block.item
				t.MessageIndex = messageIndex
				if block.busy {
					t.Running = s.Busy
				} else if block.active {
					t.Running = r.id == s.active
				}
				if block.tool {
					appendTool(t)
				} else {
					items = append(items, t)
				}
			}
		}
	}
	// Overlay authoritative live tool output without losing persisted arguments.
	for id, idx := range toolIndex {
		if t, ok := s.tools[id]; ok {
			ordinal := items[idx].MessageIndex
			if items[idx].final {
				// A completed message/history result supersedes an older partial
				// execution event, but may omit details known from that event.
				items[idx] = mergeTool(items[idx], t)
			} else {
				items[idx] = mergeTool(t, items[idx])
			}
			items[idx].MessageIndex = ordinal
		}
	}
	// A start may precede the assistant snapshot. Preserve arrival order below.
	for _, id := range s.toolIDs {
		if _, ok := toolIndex[id]; !ok {
			items = append(items, s.tools[id])
		}
	}
	for i := range items {
		if items[i].Role == "tool" {
			estimateItem(&items[i])
		}
	}
	s.Items = items
	s.MessageCount = messageIndex
}
