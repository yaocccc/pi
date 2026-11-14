package session

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// snapshotCache is immutable after construction and scoped to one State's most
// recently applied entries snapshot. It retains parsed active-branch messages
// and metadata, not a second copy of the complete RPC payload or a UI snapshot.
// Reconciliation with live records, tool details, state/stats and extensions
// still runs for every Prepare/ApplySnapshot pair.
type snapshotCache struct {
	digest   [sha256.Size]byte
	id, file string
	leaf     string
	path     []entry // leaf to root; Message bodies live only in messages
	pathIDs  map[string]bool
	messages map[string]cachedMessage
}

type cachedMessage struct {
	message message
	key     string
}

func (s *State) parsedSnapshot(raw json.RawMessage) (*snapshotCache, error) {
	digest := sha256.Sum256(raw) // Includes leafId, metadata and every entry body.
	if c := s.entriesCache; c != nil && c.digest == digest && c.id == s.ID && c.file == s.File {
		return c, nil
	}
	var v struct {
		Entries []entry         `json:"entries"`
		LeafID  json.RawMessage `json:"leafId"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
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
			return nil, err
		}
	}
	c, err := s.snapshotFromEntries(v.Entries, leaf)
	if err == nil {
		c.digest = digest
	}
	return c, err
}

// Native files already contain decoded entries. Sharing branch validation and
// message preparation avoids manufacturing and reparsing an RPC envelope.
func (s *State) snapshotFromEntries(entries []entry, leaf string) (*snapshotCache, error) {
	index := make(map[string]entry, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			if _, exists := index[e.ID]; exists {
				return nil, fmt.Errorf("duplicate session entry %s", e.ID)
			}
			index[e.ID] = e
		}
	}
	c := &snapshotCache{id: s.ID, file: s.File, leaf: leaf, pathIDs: make(map[string]bool), messages: make(map[string]cachedMessage)}
	for id := c.leaf; id != ""; {
		if c.pathIDs[id] {
			return nil, fmt.Errorf("cycle in session branch at %s", id)
		}
		c.pathIDs[id] = true
		e, ok := index[id]
		if !ok {
			return nil, fmt.Errorf("missing session entry %s", id)
		}
		if e.Type == "message" {
			var m message
			if err := json.Unmarshal(e.Message, &m); err != nil {
				return nil, fmt.Errorf("entry %s: %w", e.ID, err)
			}
			c.messages[id] = cachedMessage{message: m, key: keyOf(m)}
		}
		e.Message = nil
		c.path = append(c.path, e)
		if e.ParentID == nil {
			break
		}
		id = *e.ParentID
	}
	return c, nil
}
