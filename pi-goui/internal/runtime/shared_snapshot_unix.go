//go:build unix

package runtime

import (
	"bytes"
	"encoding/json"
)

// Retain only the unfinished message: committed history comes from get_entries.
// Native full-message updates replace the prefix; delta-only endpoints retain a
// bounded replay. If that budget is exhausted, attaching fails explicitly until
// a complete message checkpoint arrives, rather than silently losing its prefix.
func (s *brokerSession) retainLiveMessage(raw json.RawMessage) {
	var event struct {
		Type    string
		Message json.RawMessage
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	switch event.Type {
	case "message_end", "agent_settled":
		s.liveReplay, s.liveReplayErr = eventBacklog[json.RawMessage]{}, nil
		return
	case "message_start":
		s.liveReplay, s.liveReplayErr = eventBacklog[json.RawMessage]{}, nil
	case "message_update":
		var message struct{ Role string }
		if json.Unmarshal(event.Message, &message) == nil && message.Role != "" {
			// Replaying the full message and its delta would duplicate text.
			raw = brokerJSON(map[string]any{"type": "message_start", "message": event.Message})
			s.liveReplay, s.liveReplayErr = eventBacklog[json.RawMessage]{}, nil
		}
	default:
		return
	}
	if s.liveReplayErr == nil {
		if s.liveReplay.accepts(len(raw)) {
			s.liveReplay.append(bytes.Clone(raw), len(raw))
		} else {
			s.liveReplayErr = s.liveReplay.overflow("live message replay", len(raw))
			s.liveReplay = eventBacklog[json.RawMessage]{}
		}
	}
}
