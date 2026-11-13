package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

type inspectionMessage struct{ Role, StopReason string }

func (m *inspectionMessage) UnmarshalJSON(raw []byte) error {
	// Match RawMessage's last-key semantics: a repeated message field replaces
	// the previous object, rather than inheriting its role or stop reason.
	type fields inspectionMessage
	var value fields
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*m = inspectionMessage(value)
	return nil
}

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
	limited := &io.LimitedReader{R: f, N: limit + 1}
	reader := bufio.NewReaderSize(limited, 64*1024)
	// Sending needs branch topology and completion status, not message bodies,
	// tool projections, hashes or display items. Retain only these small fields.
	type inspectionEntry struct {
		Type, ID string
		ParentID *string `json:"parentId"`
		Message  inspectionMessage
	}
	index := map[string]inspectionEntry{}
	leaf := ""
	for line := 1; ; line++ {
		raw, readErr := readRecord(reader)
		if limited.N == 0 {
			return false, fmt.Errorf("session exceeds 64 MiB inspection limit")
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return false, readErr
		}
		if raw = bytes.TrimSpace(raw); len(raw) > 0 {
			var e inspectionEntry
			// Unlike rendering, a send guard must reject an incomplete final
			// record: a writer may be appending a new, unfinished turn.
			if err := json.Unmarshal(raw, &e); err != nil {
				return false, fmt.Errorf("%s:%d: %w", path, line, err)
			}
			if e.Type != "session" && e.ID != "" {
				if _, exists := index[e.ID]; exists {
					return false, fmt.Errorf("duplicate session entry %s", e.ID)
				}
				index[e.ID], leaf = e, e.ID
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	unfinished, found := false, false
	seen := map[string]bool{}
	for leaf != "" {
		if seen[leaf] {
			return false, fmt.Errorf("cycle in session parent chain at %s", leaf)
		}
		seen[leaf] = true
		e, ok := index[leaf]
		if !ok {
			return false, fmt.Errorf("missing session entry %s", leaf)
		}
		if !found && e.Type == "message" {
			switch e.Message.Role {
			case "user", "toolResult":
				unfinished, found = true, true
			case "assistant":
				found = true
				switch e.Message.StopReason {
				case "stop", "length", "error", "aborted":
				default:
					unfinished = true
				}
			}
		}
		// Validate the whole branch even after finding a terminal message.
		leaf = ""
		if e.ParentID != nil {
			leaf = *e.ParentID
		}
	}
	return unfinished, nil
}
