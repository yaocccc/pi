package session

import "encoding/json"

// MetadataRevision checkpoints event-derived state before a snapshot query.
// The query's state response may precede events covered by its history fence.
func (s *State) MetadataRevision() uint64 { return s.metadataRevision }

// observeMetadataEvent records every field of get_state that an event updates.
// Record updates even when the value is unchanged: a start/end pair can return
// to the checkpoint's value while get_state sampled the intermediate value.
func (s *State) observeMetadataEvent(kind string) {
	var fields []string
	switch kind {
	case "agent_start":
		fields = []string{"isStreaming"}
	case "agent_settled":
		fields = []string{"isStreaming", "isCompacting"}
	case "compaction_start", "compaction_end":
		fields = []string{"isCompacting"}
	case "queue_update":
		fields = []string{"steering", "followUp"}
	case "thinking_level_changed":
		fields = []string{"thinkingLevel"}
	case "session_info_changed":
		fields = []string{"sessionName"}
	}
	if len(fields) == 0 {
		return
	}
	s.metadataRevision++
	if s.metadataChanges == nil {
		s.metadataChanges = make(map[string]uint64)
	}
	for _, field := range fields {
		s.metadataChanges[field] = s.metadataRevision
	}
}

// PrepareAfter preserves metadata events processed since the query checkpoint.
// Its receiver is captured at the ordered history fence, so these field values
// and the entries cover the same event prefix. Other snapshot fields (including
// identity and model) still come from get_state. No live State is read by the
// asynchronous preparation command.
func (p SnapshotPreparation) PrepareAfter(state, entries, stats json.RawMessage, revision uint64) (*PreparedSnapshot, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(state, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	var id string
	_ = json.Unmarshal(fields["sessionId"], &id)
	if id != "" && p.base.ID != "" && id != p.base.ID {
		return p.Prepare(state, entries, stats)
	}
	values := map[string]any{
		"isStreaming":   p.base.Busy || p.base.runActive,
		"isCompacting":  p.base.Compacting,
		"steering":      p.base.Steering,
		"followUp":      p.base.FollowUp,
		"thinkingLevel": p.base.Thinking,
		"sessionName":   p.base.Name,
	}
	for field, changed := range p.base.metadataChanges {
		if changed > revision {
			value, err := json.Marshal(values[field])
			if err != nil {
				return nil, err
			}
			fields[field] = value
		}
	}
	state, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return p.Prepare(state, entries, stats)
}
