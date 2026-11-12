package session

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

// WorkerTranscript is an opaque local recording identifier, never a session path.
type WorkerTranscript struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Incomplete bool   `json:"incomplete,omitempty"`
	Error      string `json:"error,omitempty"`
}

var workerTranscriptID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (d WorkerTranscript) Valid() bool { return d.Version == 1 && workerTranscriptID.MatchString(d.ID) }

// Cursor is owned by one asynchronous read at a time. It contains no UI state.
// Reads are bounded and return complete records only; a trailing partial record
// is retained until the producer appends its LF.
type WorkerTranscriptCursor struct {
	offset       int64
	tail, anchor []byte
	file         os.FileInfo
	header       bool
}
type WorkerTranscriptBatch struct {
	Reset   bool
	Records []json.RawMessage
	More    bool
}

const workerTranscriptLimit = 65 << 20

func (c WorkerTranscriptCursor) Read(agentDir string, d WorkerTranscript) (WorkerTranscriptCursor, WorkerTranscriptBatch, error) {
	var batch WorkerTranscriptBatch
	if !d.Valid() {
		return c, batch, errors.New("invalid Worker transcript descriptor")
	}
	path := filepath.Join(agentDir, "worker-transcripts")
	info, err := os.Lstat(path)
	if err != nil {
		return c, batch, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return c, batch, errors.New("unsafe Worker transcript directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return c, batch, err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return c, batch, errors.New("Worker transcript directory changed")
	}
	name := d.ID + ".jsonl"
	info, err = root.Lstat(name)
	if err != nil {
		return c, batch, err
	}
	if !info.Mode().IsRegular() {
		return c, batch, errors.New("Worker transcript is not a regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return c, batch, err
	}
	defer f.Close()
	actual, err = f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return c, batch, errors.New("Worker transcript file changed")
	}
	if actual.Size() > workerTranscriptLimit {
		return c, batch, errors.New("Worker transcript exceeds recording limit")
	}
	reset := c.file == nil || !os.SameFile(c.file, actual) || actual.Size() < c.offset
	if !reset && len(c.anchor) > 0 {
		buf := make([]byte, len(c.anchor))
		n, _ := f.ReadAt(buf, c.offset-int64(len(buf)))
		reset = n != len(buf) || !bytes.Equal(buf, c.anchor)
	}
	if reset {
		c = WorkerTranscriptCursor{}
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
	if len(pending) > workerTranscriptLimit {
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

func (c WorkerTranscriptCursor) Partial() bool { return len(c.tail) > 0 }
