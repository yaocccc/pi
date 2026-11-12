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
	var data struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  string            `json:"leafId"`
	}
	if err := json.Unmarshal(snapshot, &data); err != nil {
		return out, err
	}
	type entry struct {
		ID, Type string
		ParentID *string
		Message  message
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
	index := slices.Index(branch, selected)
	if index < 0 || entries[selected].Type != "message" || entries[selected].Message.Role != "user" {
		return out, errors.New("selected user message is no longer on the active branch")
	}
	end := len(branch)
	for i := index + 1; i < len(branch); i++ {
		e := entries[branch[i]]
		if e.Type == "message" && e.Message.Role == "user" {
			end = i
			break
		}
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
	header, err = json.Marshal(map[string]any{"type": "session", "version": 3, "id": out.ID, "timestamp": now.Format(time.RFC3339Nano), "cwd": cwd, "parentSession": source})
	if err != nil {
		return out, err
	}
	prefix := make([]json.RawMessage, 0, end+1)
	var parent *string
	for _, id := range branch[:end] {
		prefix = append(prefix, raw[id])
		copyID := id
		parent = &copyID
	}
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
