package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Transcript is an opaque local recording identifier, never a session path.
type Transcript struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Incomplete bool   `json:"incomplete,omitempty"`
	Error      string `json:"error,omitempty"`
}

var transcriptID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (d Transcript) Valid() bool { return d.Version == 1 && transcriptID.MatchString(d.ID) }

// Cursor is owned by one asynchronous read at a time. It contains no UI state.
// Reads are bounded and return complete records only; a trailing partial record
// is retained until the producer appends its LF.
type Cursor struct {
	offset       int64
	tail, anchor []byte
	file         os.FileInfo
	header       bool
	primary      bool // Never fall back to an older copy after opening the new location.
}
type Batch struct {
	Reset   bool
	Records []json.RawMessage
	More    bool
}

const transcriptLimit = 65 << 20

func (c Cursor) Read(agentDir string, d Transcript) (Cursor, Batch, error) {
	var batch Batch
	if !d.Valid() {
		return c, batch, errors.New("invalid Worker transcript descriptor")
	}
	// Match the writer: recordings live beside the configured agent directory.
	agentPath, err := filepath.Abs(agentDir)
	if err != nil {
		return c, batch, err
	}
	f, actual, err := openTranscript(filepath.Join(filepath.Dir(agentPath), "worker-transcripts"), d.ID)
	primary := err == nil
	if errors.Is(err, os.ErrNotExist) && !c.primary {
		// Compatibility only: new writers and retention never touch this root.
		f, actual, err = openTranscript(filepath.Join(agentPath, "worker-transcripts"), d.ID)
	}
	if err != nil {
		return c, batch, err
	}
	defer f.Close()
	c.primary = primary
	return c.readFile(f, actual, d)
}

// Only a missing root/file permits fallback. An existing but unsafe, unreadable
// or invalid new record must never be bypassed by a same-ID legacy recording.
func openTranscript(path, id string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("unsafe Worker transcript directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open Worker transcript directory: %v", err)
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return nil, nil, errors.New("Worker transcript directory changed")
	}
	name := id + ".jsonl"
	info, err = root.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("Worker transcript is not a regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open Worker transcript file: %v", err)
	}
	actual, err = f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		f.Close()
		return nil, nil, errors.New("Worker transcript file changed")
	}
	return f, actual, nil
}

func (c Cursor) readFile(f *os.File, actual os.FileInfo, d Transcript) (Cursor, Batch, error) {
	var batch Batch
	if actual.Size() > transcriptLimit {
		return c, batch, errors.New("Worker transcript exceeds recording limit")
	}
	reset := c.file == nil || !os.SameFile(c.file, actual) || actual.Size() < c.offset
	if !reset && len(c.anchor) > 0 {
		buf := make([]byte, len(c.anchor))
		n, _ := f.ReadAt(buf, c.offset-int64(len(buf)))
		reset = n != len(buf) || !bytes.Equal(buf, c.anchor)
	}
	if reset {
		c = Cursor{primary: c.primary}
		batch.Reset = true
	}
	c.file = actual
	size := min(int64(4<<20), max(int64(0), actual.Size()-c.offset))
	data := make([]byte, size)
	n, err := f.ReadAt(data, c.offset)
	if err != nil && err != io.EOF {
		return c, batch, err
	}
	data = data[:n]
	c.offset += int64(n)
	if n > 0 {
		combined := append(append([]byte(nil), c.anchor...), data...)
		c.anchor = append([]byte(nil), combined[max(0, len(combined)-64):]...)
	}
	pending := append(append([]byte(nil), c.tail...), data...)
	if len(pending) > transcriptLimit {
		return c, batch, errors.New("Worker transcript record exceeds limit")
	}
	for {
		i := bytes.IndexByte(pending, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSuffix(pending[:i], []byte{'\r'})
		pending = pending[i+1:]
		var record struct {
			Type    string
			Version int
			ID      string
			Entries json.RawMessage
		}
		if json.Unmarshal(line, &record) != nil || record.Type == "" {
			return c, batch, errors.New("invalid Worker transcript record")
		}
		if !c.header {
			if record.Type != "worker_transcript_header" || record.Version != 1 || record.ID != d.ID || len(record.Entries) == 0 || record.Entries[0] != '[' {
				return c, batch, errors.New("invalid Worker transcript header")
			}
			c.header = true
		} else if record.Type == "worker_transcript_header" {
			return c, batch, fmt.Errorf("unexpected Worker transcript header")
		}
		batch.Records = append(batch.Records, append(json.RawMessage(nil), line...))
	}
	c.tail = append([]byte(nil), pending...)
	batch.More = c.offset < actual.Size()
	return c, batch, nil
}

func (c Cursor) Partial() bool { return len(c.tail) > 0 }
