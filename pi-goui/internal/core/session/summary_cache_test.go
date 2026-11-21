package session

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type summaryReadCounter struct {
	io.Reader
	bytes int
}

func (r *summaryReadCounter) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += n
	return n, err
}

func TestSummaryCacheSkipsUnchangedBody(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	raw := append(summaryFixture(cwd, "Original", "/parent", "first"),
		`{"type":"message","message":{"role":"toolResult","content":"`+strings.Repeat("x", 256*1024)+`"}}`+"\n"...)
	path := writeSessionFixture(t, root, "cwd/session", raw)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	r := &summaryReadCounter{Reader: f}
	want, err := readSessionSummary(r, path, cwd, opened, cachedSummary{})
	if err != nil || r.bytes != len(raw) || want.FirstMessage != "first" || want.Name != "Original" {
		t.Fatalf("cold summary = %+v, read %d/%d bytes, err=%v", want, r.bytes, len(raw), err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	r.bytes = 0
	list, cache, err := (SummaryCache{}).Refresh(root, cwd)
	if err != nil || len(list) != 1 || list[0].Fork == nil {
		t.Fatalf("Refresh: %+v, %v", list, err)
	}
	list[0].Fork.SourceID = "modified by caller"
	got, err := readSessionSummary(r, path, cwd, opened, cache.files[path])
	if err != nil || r.bytes != 0 || got.Name != want.Name || got.FirstMessage != want.FirstMessage || got.Fork == nil || *got.Fork != *want.Fork {
		t.Fatalf("warm summary = %+v, read %d bytes, err=%v", got, r.bytes, err)
	}
}

func TestSummaryCacheRefreshFileChanges(t *testing.T) {
	root, cwd, other := t.TempDir(), t.TempDir(), t.TempDir()
	raw := summaryFixture(cwd, "Original", "/parent", "first")
	path := writeSessionFixture(t, root, "cwd/session", raw)
	writeSessionFixture(t, root, "other/session", summaryFixture(other, "Other", "", "other"))
	var cache SummaryCache
	refresh := func(dir string) []Summary {
		t.Helper()
		list, next, err := cache.Refresh(root, dir)
		if err != nil {
			t.Fatal(err)
		}
		cache = next
		return list
	}
	assert := func(dir, name, first string) {
		t.Helper()
		got := refresh(dir)
		if len(got) != 1 || got[0].Name != name || got[0].FirstMessage != first || got[0].Cwd != dir {
			t.Fatalf("summary for %s: %+v, want name %q, first %q", dir, got, name, first)
		}
	}
	write := func(path string, raw []byte, ts time.Time) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Unix(1_700_000_000, 0)
	write(path, raw, stamp)
	assert(cwd, "Original", "first")
	assert(cwd, "Original", "first")

	// A growing log can be seen before all of its final record is persisted.
	appendRecord := func(text string) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(text)
		if closeErr := f.Close(); err != nil || closeErr != nil {
			t.Fatalf("append: %v, close: %v", err, closeErr)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	appendRecord(`{"type":"session_info","name":"Updated`)
	assert(cwd, "Original", "first")
	appendRecord("\"}\n")
	assert(cwd, "Updated", "first") // Only the size has changed.
	write(path, raw, stamp)
	assert(cwd, "Original", "first") // Truncation drops the latest name.

	updated := summaryFixture(cwd, "New name", "/parent", "start")
	if len(updated) != len(raw) {
		t.Fatal("fixture must preserve size in the next two cases")
	}
	stamp = stamp.Add(time.Second)
	write(path, updated, stamp)
	assert(cwd, "New name", "start") // Same inode and size, newer mtime.

	replacement := filepath.Join(root, "replacement")
	write(replacement, raw, stamp)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	assert(cwd, "Original", "first") // Same size and mtime, different inode.
	assert(other, "Other", "other")  // A snapshot cannot bypass directory filtering.
	assert(cwd, "Original", "first")

	if err := os.Rename(path, replacement); err != nil {
		t.Fatal(err)
	}
	if got := refresh(cwd); len(got) != 0 || len(cache.files) != 0 {
		t.Fatalf("deleted log retained: %+v", got)
	}
}

func TestSummaryCacheDoesNotFollowReplacementSymlink(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := writeSessionFixture(t, root, "session", summaryFixture(cwd, "Original", "", "first"))
	_, cache, err := (SummaryCache{}).Refresh(root, cwd)
	if err != nil || len(cache.files) != 1 {
		t.Fatalf("prime cache: %d files, %v", len(cache.files), err)
	}
	target := filepath.Join(root, "renamed")
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if got, next, err := cache.Refresh(root, cwd); err != nil || len(got) != 0 || len(next.files) != 0 {
		t.Fatalf("symlink retained or followed: %+v, %v", got, err)
	}
}
