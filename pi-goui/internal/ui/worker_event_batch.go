package ui

import (
	"encoding/json"
	"strings"
)

// A read already batches disk events. Coalesce only adjacent delta-only updates
// of the same content block before projecting State: N tokens otherwise rebuild
// the entire historical item list N times. Snapshots, starts/ends, tool results
// and every other event remain ordered barriers. The saved log is not changed.
func workerEventBatch(records []json.RawMessage) []json.RawMessage {
	type delta struct {
		Type         string `json:"type"`
		ContentIndex int    `json:"contentIndex"`
		Delta        string `json:"delta"`
	}
	type event struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message,omitempty"`
		Update  delta           `json:"assistantMessageEvent"`
	}
	var out []json.RawMessage
	var pending event
	var original json.RawMessage
	var text strings.Builder
	count := 0
	flush := func() {
		if count == 1 {
			out = append(out, original)
		} else if count > 1 {
			pending.Update.Delta = text.String()
			raw, _ := json.Marshal(pending)
			out = append(out, raw)
		}
		count = 0
		text.Reset()
	}
	for _, raw := range records {
		var e event
		if json.Unmarshal(raw, &e) != nil || e.Type != "message_update" || (len(e.Message) > 0 && string(e.Message) != "null") || (e.Update.Type != "text_delta" && e.Update.Type != "thinking_delta" && e.Update.Type != "toolcall_delta") {
			flush()
			out = append(out, raw)
			continue
		}
		if count > 0 && (pending.Update.Type != e.Update.Type || pending.Update.ContentIndex != e.Update.ContentIndex) {
			flush()
		}
		if count == 0 {
			pending = e
			original = raw
		}
		text.WriteString(e.Update.Delta)
		count++
	}
	flush()
	return out
}
