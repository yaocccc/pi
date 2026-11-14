package session

import (
	"bufio"
	"bytes"
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
	return readEntries(f, path)
}

// ReadPreview reads a saved header and entries from one stable descriptor.
// This is a read-only display snapshot, never an authority for sending.
func ReadPreview(path string) (Header, json.RawMessage, error) {
	header, f, _, opened, err := openPreview(path)
	if err != nil {
		return header, nil, err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return header, nil, err
	}
	entries, err := readEntries(f, path)
	if err != nil {
		return header, nil, err
	}
	if err := checkPreviewFile(path, opened); err != nil {
		return header, nil, err
	}
	return header, entries, nil
}

// openPreview returns a reader immediately after the validated header. Both
// preview APIs use the same descriptor/identity checks; neither follows a
// symlink at the requested path nor accepts a replacement during the read.
func openPreview(path string) (Header, *os.File, *bufio.Reader, os.FileInfo, error) {
	var header Header
	before, err := os.Lstat(path)
	if err != nil {
		return header, nil, nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return header, nil, nil, nil, fmt.Errorf("history is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return header, nil, nil, nil, err
	}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return header, nil, nil, nil, fmt.Errorf("history changed before reading")
	}
	r := bufio.NewReaderSize(f, 64*1024)
	raw, err := readRecord(r)
	if err != nil && !errors.Is(err, io.EOF) {
		return header, nil, nil, nil, err
	}
	if err := json.Unmarshal(raw, &header); err != nil || header.Type != "session" || header.ID == "" {
		return Header{}, nil, nil, nil, fmt.Errorf("history has no valid session header")
	}
	normalizeOrigin(&header)
	ok = true
	return header, f, r, opened, nil
}

func checkPreviewFile(path string, opened os.FileInfo) error {
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, after) || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("history changed while reading")
	}
	return nil
}

// readPreviewEntries decodes each native entry once, retaining raw message and
// metadata fields for the same branch/projection logic as get_entries.
func readPreviewEntries(path string) (Header, []entry, string, error) {
	header, f, r, opened, err := openPreview(path)
	if err != nil {
		return header, nil, "", err
	}
	defer f.Close()
	var entries []entry
	leaf := ""
	for line := 2; ; line++ {
		raw, err := readRecord(r)
		if len(bytes.TrimSpace(raw)) > 0 {
			var e entry
			parseErr := json.Unmarshal(raw, &e)
			if parseErr != nil && e.Type == "session" {
				// Extra headers are omitted by ReadFile too. Their non-entry
				// fields need not conform to the entry schema.
				var header struct{ Type, ID string }
				parseErr = json.Unmarshal(raw, &header)
			}
			if parseErr != nil {
				if errors.Is(err, io.EOF) && strings.Contains(parseErr.Error(), "unexpected end") {
					break
				}
				return header, nil, "", fmt.Errorf("%s:%d: %w", path, line, parseErr)
			}
			if e.Type != "session" {
				entries = append(entries, e)
				if e.ID != "" {
					leaf = e.ID
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return header, nil, "", err
		}
	}
	if err := checkPreviewFile(path, opened); err != nil {
		return header, nil, "", err
	}
	return header, entries, leaf, nil
}

func readEntries(source io.Reader, path string) (json.RawMessage, error) {
	r := bufio.NewReaderSize(source, 64*1024)
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
	return listSessions(filepath.Join(root, "sessions"), cwd)
}

func listSessions(dir, cwd string) ([]Summary, error) {
	var result []Summary
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
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		opened, err := f.Stat()
		if err != nil {
			return err
		}
		if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			return fmt.Errorf("session file changed while opening: %s", path)
		}
		r := bufio.NewReaderSize(f, 32*1024)
		raw, err := readRecord(r)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		var header Header
		if json.Unmarshal(raw, &header) != nil || header.Type != "session" {
			return nil
		}
		if cwd != "" && filepath.Clean(header.Cwd) != cwd {
			return nil
		}
		normalizeOrigin(&header)
		v := Summary{ID: header.ID, Path: path, Cwd: header.Cwd, Modified: opened.ModTime().UnixMilli(), ParentSession: header.ParentSession, Fork: header.Fork}
		for !errors.Is(err, io.EOF) {
			raw, err = readRecord(r)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			// After the first user message, only session_info can change the
			// summary. Avoid decoding large tool/assistant payloads. Any JSON
			// spelling of that type contains its literal string or a Unicode
			// escape; the latter must still take the full parsing path.
			if v.FirstMessage != "" && !bytes.Contains(raw, []byte(`"session_info"`)) && !bytes.Contains(raw, []byte(`\u`)) {
				continue
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
		// Reject path replacement while allowing the active writer to append.
		current, pathErr := os.Lstat(path)
		if pathErr != nil {
			return pathErr
		}
		if !current.Mode().IsRegular() || !os.SameFile(opened, current) {
			return fmt.Errorf("session file changed while reading: %s", path)
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
