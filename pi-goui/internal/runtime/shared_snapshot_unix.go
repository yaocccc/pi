//go:build unix

package runtime

import (
	"bytes"
	"encoding/json"
)

// snapshotJSON embeds native JSON without expanding HTML characters. Escaping
// them again could multiply a legal native payload beyond the two-response
// shared frame budget. The request is a uint64, so envelope overhead is bounded.
func snapshotJSON(v any) json.RawMessage {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(v)
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
}

func historySnapshotFrame(request uint64, state, entries json.RawMessage) json.RawMessage {
	return snapshotJSON(struct {
		Type    string          `json:"type"`
		Request uint64          `json:"request"`
		State   json.RawMessage `json:"state"`
		Entries json.RawMessage `json:"entries"`
	}{"goui_history_snapshot", request, state, entries})
}

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
