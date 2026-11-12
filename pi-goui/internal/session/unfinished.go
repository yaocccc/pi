package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// UnfinishedFile examines only the persisted branch. It cannot distinguish a
// running agent from an interrupted run, or observe unpersisted tree navigation.
// Errors are deliberately not interpreted as a completed conversation.
func UnfinishedFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	const limit = 64 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return false, err
	}
	if len(data) > limit {
		return false, fmt.Errorf("session exceeds 64 MiB inspection limit")
	}
	// ReadFile tolerates a partially appended final record for rendering. For a
	// send guard that tail is uncertainty, not evidence of completion.
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		tail := trimmed[bytes.LastIndexByte(trimmed, '\n')+1:]
		if !json.Valid(tail) {
			return false, fmt.Errorf("session has an incomplete final record")
		}
	}
	raw, err := readEntries(bytes.NewReader(data), path)
	if err != nil {
		return false, err
	}
	var s State
	if err := s.LoadEntries(raw); err != nil {
		return false, err
	}
	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		switch r.msg.Role {
		case "user", "toolResult":
			return true, nil
		case "assistant":
			switch r.msg.StopReason {
			case "stop", "length", "error", "aborted":
				return false, nil
			default:
				return true, nil
			}
		}
	}
	return false, nil
}
