package session

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MutationFile binds an explicit selector action to a validated native v3 file.
// It never creates, rewrites, migrates or follows a symlink. The UI reserves its
// path and closes its own writer before removal. Independent applications must
// still not edit this same history concurrently (pi has no shared writer lock).
type MutationFile struct {
	path, id string
	info     os.FileInfo
	digest   [32]byte
	leaf     *string
	ids      map[string]bool
}

func regularSessionPath(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return nil, errors.New("select an absolute .jsonl session file")
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("session mutations do not follow symlinks")
		}
		if p == filepath.Clean(path) && !info.Mode().IsRegular() {
			return nil, errors.New("session is not a regular file")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return os.Lstat(path)
}

func InspectMutationFile(path, expectedID string) (*MutationFile, error) {
	info, err := regularSessionPath(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > 512*1024*1024 {
		return nil, errors.New("session exceeds 512 MiB mutation validation limit")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("session file changed while opening")
	}
	hash := sha256.New()
	r := bufio.NewReader(io.TeeReader(io.LimitReader(f, info.Size()+1), hash))
	out := &MutationFile{path: path, info: info, ids: map[string]bool{}}
	for line := 0; ; line++ {
		raw, err := readRecord(r)
		if len(raw) == 0 && errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("session must contain complete newline-terminated JSONL records: %w", err)
		}
		var e struct {
			Type, ID string
			Version  int
			ParentID *string `json:"parentId"`
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("invalid session record %d: %w", line+1, err)
		}
		if line == 0 {
			if e.Type != "session" || e.Version != 3 || e.ID == "" {
				return nil, errors.New("session mutation requires a native version 3 header")
			}
			out.id = e.ID
			if expectedID != "" && e.ID != expectedID {
				return nil, errors.New("selected session identity changed")
			}
		} else {
			if e.Type == "session" || e.Type == "" || e.ID == "" || out.ids[e.ID] {
				return nil, errors.New("invalid or duplicate native session entry")
			}
			if e.ParentID != nil && !out.ids[*e.ParentID] {
				return nil, errors.New("session entry references a missing parent")
			}
			out.ids[e.ID] = true
			id := e.ID
			out.leaf = &id
		}
	}
	if out.id == "" {
		return nil, errors.New("empty session file")
	}
	copy(out.digest[:], hash.Sum(nil))
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("session changed during validation; retry")
	}
	return out, nil
}

// SameFile permits the owned RPC writer to finish appending before deletion,
// while rejecting replacement of the selected inode or native session ID.
func (f *MutationFile) SameFile(other *MutationFile) bool {
	return other != nil && f.path == other.path && f.id == other.id && os.SameFile(f.info, other.info)
}
func (f *MutationFile) unchanged() error {
	current, err := InspectMutationFile(f.path, f.id)
	if err != nil {
		return err
	}
	if !f.SameFile(current) || f.digest != current.digest {
		return errors.New("session changed since validation; retry")
	}
	return nil
}
func (f *MutationFile) Rename(name string) error {
	if err := f.unchanged(); err != nil {
		return err
	}
	name = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(name))
	var id string
	for {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		id = hex.EncodeToString(b)
		if !f.ids[id] {
			break
		}
	}
	// Native field spellings are lowercase; preserve the pre-existing bytes.
	raw, err := json.Marshal(map[string]any{"type": "session_info", "id": id, "parentId": f.leaf, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "name": name})
	if err != nil {
		return err
	}
	handle, err := os.OpenFile(f.path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(f.info, info) || info.Size() != f.info.Size() || !info.ModTime().Equal(f.info.ModTime()) {
		return errors.New("session changed before append")
	}
	if _, err = regularSessionPath(f.path); err != nil {
		return err
	}
	raw = append(raw, '\n')
	n, err := handle.Write(raw)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	return handle.Sync()
}
func (f *MutationFile) Delete() error {
	if err := f.unchanged(); err != nil {
		return err
	}
	return os.Remove(f.path)
}
