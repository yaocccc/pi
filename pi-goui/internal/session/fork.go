package session

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Fork is a separately persisted native v3 session, never a mutation of its
// source. Entries is ready for a fresh UI projection.
type Fork struct {
	ID, Path, Name, Cwd string
	Entries             json.RawMessage
	Origin              Header
}

// ForkPreview is derived from the same active-branch prefix used for persistence.
// PreviewFork performs no filesystem operations.
type ForkPreview struct {
	Prefix                                     []json.RawMessage
	LeafID, NextUserID, Text, Reply, Timestamp string
	Turn, ExcludedTurns                        int
	ReplyCompleted                             bool
	// Checkpoints compare only the copied prefix and next user boundary;
	// later streaming messages are deliberately not part of this history.
	history      []record
	prefixIDs    []string
	branchIDs    map[string]bool
	snapshotLeaf string
}

// ForkCheckpoint is an immutable view of the currently displayed branch. Record
// messages are replaced, not mutated in place, by State.Apply/LoadEntries.
// Keeping both the request and response checkpoints detects changed history
// without treating a delta in a later turn as a stale fork selection.
type ForkCheckpoint struct {
	records      []record
	persisted    int
	leaf, active string
	pathIDs      map[string]bool
}

func (s *State) ForkCheckpoint() ForkCheckpoint {
	return ForkCheckpoint{records: slices.Concat(s.records, s.live), persisted: len(s.records), leaf: s.leaf, active: s.active, pathIDs: s.pathIDs}
}

func forkMessageKey(m message) string {
	// LoadEntries can enrich a persisted tool result with live UI details.
	// Those details are not evidence that the persisted history changed.
	if m.Role == "toolResult" {
		m.Details = nil
	}
	return keyOf(m)
}

// Matches requires the same ordered history and user boundary on a compatible
// active branch. A descendant leaf is normal while a later turn is streaming;
// an ancestor/sibling navigation which removes the boundary cannot match.
func (c ForkCheckpoint) Matches(p ForkPreview) bool {
	if p.NextUserID == "" || !p.ReplyCompleted || c.leaf == "" || (!p.branchIDs[c.leaf] && !c.pathIDs[p.snapshotLeaf]) {
		return false
	}
	if len(c.records) < len(p.history) {
		return false
	}
	// Verify native ancestry up to the cached leaf as well as message content.
	// The rest may be a live suffix which has not been reconciled with IDs yet.
	for _, id := range p.prefixIDs {
		if !c.pathIDs[id] {
			return false
		}
		if id == c.leaf {
			break
		}
	}
	for i, expected := range p.history {
		actual := c.records[i]
		if actual.msg.partial || actual.id == c.active || actual.summary != expected.summary || forkMessageKey(actual.msg) != forkMessageKey(expected.msg) {
			return false
		}
		// A persisted record may have a live alias, but its native entry must
		// remain on this branch. Live suffix records have no native ID yet;
		// their exact ordered message content is the available identity.
		if i < c.persisted && !c.pathIDs[expected.id] {
			return false
		}
	}
	return true
}

func PreviewFork(selected string, snapshot json.RawMessage) (ForkPreview, error) {
	var out ForkPreview
	var data struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(snapshot, &data); err != nil {
		return out, err
	}
	type entry struct {
		ID, Type, Timestamp, Summary string
		ParentID                     *string
		Message                      message
	}
	entries := make(map[string]entry, len(data.Entries))
	raw := make(map[string]json.RawMessage, len(data.Entries))
	for _, r := range data.Entries {
		var e entry
		if err := json.Unmarshal(r, &e); err != nil {
			return out, err
		}
		if e.ID == "" || e.Type == "session" || raw[e.ID] != nil {
			return out, errors.New("invalid fork snapshot entry")
		}
		entries[e.ID], raw[e.ID] = e, r
	}
	var branch []string
	seen := map[string]bool{}
	for id := data.LeafID; id != ""; {
		e, ok := entries[id]
		if !ok || seen[id] {
			return out, errors.New("fork snapshot has a broken branch")
		}
		seen[id] = true
		branch = append(branch, id)
		id = ""
		if e.ParentID != nil {
			id = *e.ParentID
		}
	}
	slices.Reverse(branch)
	out.branchIDs, out.snapshotLeaf = seen, data.LeafID
	index := slices.Index(branch, selected)
	if index < 0 || entries[selected].Type != "message" || entries[selected].Message.Role != "user" {
		return out, errors.New("selected user message is no longer on the active branch")
	}
	out.Text, out.Timestamp = userText(entries[selected].Message.Content), entries[selected].Timestamp
	end := len(branch)
	for i, id := range branch {
		e := entries[id]
		if e.Type == "message" && e.Message.Role == "user" {
			if i <= index {
				out.Turn++
			} else {
				out.ExcludedTurns++
				if end == len(branch) {
					end, out.NextUserID = i, id
				}
			}
		}
	}
	out.prefixIDs = branch[:end]
	pendingTools := map[string]bool{}
	invalidToolCall := false
	for i, id := range branch[:end] {
		e := entries[id]
		out.Prefix = append(out.Prefix, raw[id])
		out.LeafID = id
		switch e.Type {
		case "message":
			out.history = append(out.history, record{id: id, msg: e.Message})
		case "compaction":
			out.history = append(out.history, record{id: id, summary: "Context compacted — earlier messages retained\n" + e.Summary})
		case "branch_summary":
			out.history = append(out.history, record{id: id, summary: "Branch summary\n" + e.Summary})
		}
		if i <= index || e.Type != "message" {
			continue
		}
		switch m := e.Message; m.Role {
		case "assistant":
			if text := textOf(m.Content); text != "" {
				out.Reply = text
			}
			for _, block := range blocks(m.Content) {
				if block.Type == "toolCall" {
					invalidToolCall = invalidToolCall || block.ID == "" || pendingTools[block.ID]
					pendingTools[block.ID] = true
				}
			}
			out.ReplyCompleted = (m.StopReason == "stop" || m.StopReason == "length") && len(pendingTools) == 0 && !invalidToolCall
		case "toolResult":
			delete(pendingTools, m.ToolCallID)
			out.ReplyCompleted = false // A final assistant reply must follow tools.
		}
	}
	if out.NextUserID != "" {
		out.history = append(out.history, record{id: out.NextUserID, msg: entries[out.NextUserID].Message})
	}
	return out, nil
}

// CreateFork copies the active branch through the selected user entry's reply,
// stopping before the next user message. The snapshot comes from get_entries,
// not the file's last leaf (which may differ after in-memory tree navigation).
func CreateFork(source, sourceID, cwd, name, selected string, snapshot json.RawMessage) (Fork, error) {
	var out Fork
	if _, err := regularSessionPath(source); err != nil {
		return out, err
	}
	f, err := os.Open(source)
	if err != nil {
		return out, err
	}
	header, readErr := readRecord(bufio.NewReader(f))
	_ = f.Close()
	if readErr != nil {
		return out, readErr
	}
	var original struct {
		Type, ID string
		Version  int
	}
	if json.Unmarshal(header, &original) != nil || original.Type != "session" || original.Version != 3 || original.ID != sourceID {
		return out, errors.New("fork source identity changed or is not a native v3 session")
	}
	preview, err := PreviewFork(selected, snapshot)
	if err != nil {
		return out, err
	}
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return out, err
	}
	uuid[6], uuid[8] = uuid[6]&0x0f|0x40, uuid[8]&0x3f|0x80
	out.ID = fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	out.Cwd, out.Name = cwd, name
	now := time.Now().UTC()
	out.Path = filepath.Join(filepath.Dir(source), now.Format("2006-01-02T15-04-05-000Z")+"_"+out.ID+".jsonl")
	provenance := &ForkProvenance{Version: 1, SourceID: sourceID, UserEntryID: selected, CutoffEntryID: preview.LeafID, Turn: preview.Turn}
	out.Origin = Header{Type: "session", ID: out.ID, Cwd: cwd, ParentSession: source, Fork: provenance}
	header, err = json.Marshal(map[string]any{"type": "session", "version": 3, "id": out.ID, "timestamp": now.Format(time.RFC3339Nano), "cwd": cwd, "parentSession": source, "piGouiFork": provenance})
	if err != nil {
		return out, err
	}
	prefix := append([]json.RawMessage(nil), preview.Prefix...)
	parent := &preview.LeafID
	// A final native session_info overrides inherited names without modifying
	// copied entries, labels, compaction references or extension metadata.
	nameID := "fork-" + out.ID
	nameEntry, _ := json.Marshal(map[string]any{"type": "session_info", "id": nameID, "parentId": parent, "timestamp": now.Format(time.RFC3339Nano), "name": name})
	prefix = append(prefix, nameEntry)
	out.Entries, err = json.Marshal(map[string]any{"entries": prefix, "leafId": nameID})
	if err != nil {
		return out, err
	}
	file, err := os.OpenFile(out.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return out, err
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(out.Path)
		}
	}()
	writer := bufio.NewWriter(file)
	for _, record := range append([]json.RawMessage{header}, prefix...) {
		if _, err = writer.Write(append(append([]byte(nil), record...), '\n')); err != nil {
			return out, err
		}
	}
	if err = writer.Flush(); err != nil {
		return out, err
	}
	if err = file.Close(); err != nil {
		return out, err
	}
	complete = true
	return out, nil
}
