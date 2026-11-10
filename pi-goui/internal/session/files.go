package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ReadFile returns get_entries-compatible JSON. Like pi on reopening a JSONL
// file, its default leaf is the last persisted entry (navigation alone is not
// persisted). Prefer live get_entries when a process has navigated its tree.
func ReadFile(path string) (json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	var entries []json.RawMessage
	var leaf *string
	line := 0
	for {
		raw, err := readRecord(r)
		line++
		if len(strings.TrimSpace(string(raw))) > 0 {
			var e struct{ Type, ID string }
			parseErr := json.Unmarshal(raw, &e)
			if parseErr != nil {
				if errors.Is(err, io.EOF) && strings.Contains(parseErr.Error(), "unexpected end") {
					break
				}
				return nil, fmt.Errorf("%s:%d: %w", path, line, parseErr)
			}
			if e.Type != "session" {
				entries = append(entries, append(json.RawMessage(nil), raw...))
				if e.ID != "" {
					id := e.ID
					leaf = &id
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if entries == nil {
		entries = []json.RawMessage{}
	}
	return json.Marshal(struct {
		Entries []json.RawMessage `json:"entries"`
		LeafID  *string           `json:"leafId"`
	}{entries, leaf})
}
func readRecord(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(out)+len(part) > 64*1024*1024 {
			return nil, fmt.Errorf("session JSONL record exceeds 64 MiB")
		}
		out = append(out, part...)
		if !errors.Is(err, bufio.ErrBufferFull) {
			return out, err
		}
	}
}

// List reads native session metadata without changing sessions or following
// symlinked directories/files. Missing session directories are normal.
func List(root, cwd string) ([]Summary, error) {
	var result []Summary
	dir := filepath.Join(root, "sessions")
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return nil, err
		}
		cwd = filepath.Clean(abs)
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == dir && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		r := bufio.NewReaderSize(f, 32*1024)
		raw, err := readRecord(r)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		var header struct{ Type, ID, Cwd string }
		if json.Unmarshal(raw, &header) != nil || header.Type != "session" {
			return nil
		}
		if cwd != "" && filepath.Clean(header.Cwd) != cwd {
			return nil
		}
		v := Summary{ID: header.ID, Path: path, Cwd: header.Cwd, Modified: info.ModTime().UnixMilli()}
		for !errors.Is(err, io.EOF) {
			raw, err = readRecord(r)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			var e entry
			if json.Unmarshal(raw, &e) != nil {
				continue
			}
			if e.Type == "session_info" {
				v.Name = e.Name
			}
			if e.Type == "message" && v.FirstMessage == "" {
				var m message
				if json.Unmarshal(e.Message, &m) == nil && m.Role == "user" {
					v.FirstMessage = userText(m.Content)
				}
			}
		}
		result = append(result, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Modified == result[j].Modified {
			return result[i].Path < result[j].Path
		}
		return result[i].Modified > result[j].Modified
	})
	return result, nil
}
