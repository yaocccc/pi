package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"
)

// Fork is a separately persisted native v3 session, never a mutation of its
// source. Entries is omitted when only the durable file was requested.
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

// ResolveUserID translates a display alias using the immutable request history.
// Persisted records retain their native ID. A not-yet-reconciled live record is
// matched by its exact ordered prefix (including timestamps), never by text.
func (c ForkCheckpoint) ResolveUserID(selected string, snapshot json.RawMessage) (string, error) {
	index := slices.IndexFunc(c.records, func(r record) bool { return r.id == selected && r.msg.Role == "user" })
	if index < 0 {
		// Choices obtained from get_fork_messages already carry native IDs;
		// PreviewFork still checks their membership in the active branch.
		return selected, nil
	}
	if id := c.records[index].entryID; id != "" {
		return id, nil
	}
	var current State
	if err := current.LoadEntries(snapshot); err != nil {
		return "", err
	}
	stale := errors.New("selected user message is no longer on the active branch")
	if len(current.records) <= index || (c.leaf != "" && !current.pathIDs[c.leaf]) {
		return "", stale
	}
	for i, expected := range c.records[:index+1] {
		actual := current.records[i]
		if expected.msg.partial || expected.summary != actual.summary || !sameForkMessage(expected.msg, actual.msg) || (expected.entryID != "" && expected.entryID != actual.entryID) {
			return "", stale
		}
	}
	return current.records[index].entryID, nil
}

// Identical decoded messages are the usual case for persisted prefixes.
// Compare their actual fields (not possibly stale keys); only canonicalize
// both bodies when different representations might still be equivalent.
func sameForkMessage(a, b message) bool {
	return reflect.DeepEqual(a, b) || forkMessageKey(a) == forkMessageKey(b)
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
		if actual.msg.partial || actual.id == c.active || actual.summary != expected.summary || !sameForkMessage(actual.msg, expected.msg) {
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

// SamePrefix proves that a previously verified checkpoint still describes the
// copied prefix using already computed record identities, without reading or
// hashing message bodies. A false result requires async Matches revalidation;
// enriched tool details can change a key without changing forkable history.
func (c ForkCheckpoint) SamePrefix(verified ForkCheckpoint, p ForkPreview) bool {
	if p.NextUserID == "" || !p.ReplyCompleted || c.leaf == "" || (!p.branchIDs[c.leaf] && !c.pathIDs[p.snapshotLeaf]) || len(c.records) < len(p.history) || len(verified.records) < len(p.history) {
		return false
	}
	for _, id := range p.prefixIDs {
		if !c.pathIDs[id] {
			return false
		}
		if id == c.leaf {
			break
		}
	}
	for i, expected := range p.history {
		actual, prior := c.records[i], verified.records[i]
		if actual.msg.partial || actual.id == c.active || actual.summary != prior.summary || actual.key != prior.key || !actual.msg.sameStreamVersion(prior.msg) || (actual.summary == "" && actual.key == "") {
			return false
		}
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

// ForkPlan owns the validated prefix. No writable raw messages or metadata
// from its review are accepted as persistence input. The parser owns its bytes
// (including when the caller reuses its RPC buffer after PrepareFork returns).
type ForkPlan struct {
	selected string
	preview  ForkPreview
}

func PrepareFork(selected string, snapshot json.RawMessage) (*ForkPlan, error) {
	preview, err := PreviewFork(selected, snapshot)
	if err != nil {
		return nil, err
	}
	return &ForkPlan{selected, preview}, nil
}

// Review exposes checkpoint/turn metadata, not the writable prefix bytes.
func (p *ForkPlan) Review() ForkPreview {
	v := p.preview
	v.Prefix = nil
	return v
}

// CreateFork copies the active branch through the selected user entry's reply,
// stopping before the next user message. The snapshot comes from get_entries,
// not the file's last leaf (which may differ after in-memory tree navigation).
func CreateFork(source, sourceID, cwd, name, selected string, snapshot json.RawMessage) (Fork, error) {
	p, err := PrepareFork(selected, snapshot)
	if err != nil {
		return Fork{}, err
	}
	return p.create(source, sourceID, cwd, name, true)
}

func (p *ForkPlan) create(source, sourceID, cwd, name string, includeEntries bool) (Fork, error) {
	var out Fork
	if p == nil || p.selected == "" || len(p.preview.Prefix) == 0 {
		return out, errors.New("missing validated fork plan")
	}
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
	preview := &p.preview
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return out, err
	}
	uuid[6], uuid[8] = uuid[6]&0x0f|0x40, uuid[8]&0x3f|0x80
	out.ID = fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	out.Cwd, out.Name = cwd, name
	now := time.Now().UTC()
	out.Path = filepath.Join(filepath.Dir(source), now.Format("2006-01-02T15-04-05-000Z")+"_"+out.ID+".jsonl")
	provenance := &ForkProvenance{Version: 1, SourceID: sourceID, UserEntryID: p.selected, CutoffEntryID: preview.LeafID, Turn: preview.Turn}
	out.Origin = Header{Type: "session", ID: out.ID, Cwd: cwd, ParentSession: source, Fork: provenance}
	header, err = json.Marshal(map[string]any{"type": "session", "version": 3, "id": out.ID, "timestamp": now.Format(time.RFC3339Nano), "cwd": cwd, "parentSession": source, "piGouiFork": provenance})
	if err != nil {
		return out, err
	}
	parent := preview.LeafID
	// A final native session_info overrides inherited names without modifying
	// copied entries, labels, compaction references or extension metadata.
	nameID := "fork-" + out.ID
	nameEntry, _ := json.Marshal(map[string]any{"type": "session_info", "id": nameID, "parentId": parent, "timestamp": now.Format(time.RFC3339Nano), "name": name})
	// Each record was already validated by PreviewFork. Build the RPC view
	// directly rather than remarshal the entire history. nameID is generated
	// exclusively from the UUID above and contains no JSON metacharacters.
	if includeEntries {
		size := len(nameEntry) + len(nameID) + 32
		for _, r := range preview.Prefix {
			size += len(r) + 1
		}
		out.Entries = append(make([]byte, 0, size), `{"entries":[`...)
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
	writeLine := func(line []byte) error {
		if _, err := writer.Write(line); err != nil {
			return err
		}
		return writer.WriteByte('\n')
	}
	if err = writeLine(header); err != nil {
		return out, err
	}
	var compact bytes.Buffer
	for _, r := range preview.Prefix {
		// A pretty-printed RPC response may contain multiline records. Native
		// JSONL requires one physical line, but common records need no copying.
		if bytes.IndexAny(r, "\r\n") >= 0 {
			compact.Reset()
			if err = json.Compact(&compact, r); err != nil {
				return out, err
			}
			r = compact.Bytes()
		}
		if err = writeLine(r); err != nil {
			return out, err
		}
		if includeEntries {
			out.Entries = append(out.Entries, r...)
			out.Entries = append(out.Entries, ',')
		}
	}
	if err = writeLine(nameEntry); err != nil {
		return out, err
	}
	if includeEntries {
		out.Entries = append(out.Entries, nameEntry...)
		out.Entries = append(out.Entries, `],"leafId":"`...)
		out.Entries = append(out.Entries, nameID...)
		out.Entries = append(out.Entries, '"', '}')
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
