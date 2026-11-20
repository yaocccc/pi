package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// RecentDirectories returns distinct existing project directories, newest first.
// Only session headers (at most 64 KiB) are read; message bodies are not scanned.
// Ties are ordered by directory path. Missing history and nonpositive limits
// return an empty list. Malformed headers and unavailable project directories
// are skipped; session traversal, I/O and file-identity errors are returned.
func RecentDirectories(root string, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	recent := make(map[string]time.Time)
	dir := filepath.Join(root, "sessions")
	err := walkSessionFiles(dir, func(path string, f *os.File, opened os.FileInfo) error {
		raw, readErr := bufio.NewReaderSize(f, 64*1024).ReadSlice('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, bufio.ErrBufferFull) {
			return readErr
		}
		if err := checkSessionFileIdentity(path, opened); err != nil {
			return err
		}
		var header struct{ Type, Cwd string }
		if errors.Is(readErr, bufio.ErrBufferFull) || json.Unmarshal(raw, &header) != nil || header.Type != "session" || !filepath.IsAbs(header.Cwd) {
			return nil
		}
		cwd := filepath.Clean(header.Cwd)
		project, err := os.Stat(cwd)
		if err != nil || !project.IsDir() {
			return nil
		}
		modified := opened.ModTime()
		if previous, exists := recent[cwd]; !exists || modified.After(previous) {
			recent[cwd] = modified
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ordered := make([]string, 0, len(recent))
	for cwd := range recent {
		ordered = append(ordered, cwd)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := recent[ordered[i]], recent[ordered[j]]
		if a.Equal(b) {
			return ordered[i] < ordered[j]
		}
		return a.After(b)
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	return ordered, nil
}
