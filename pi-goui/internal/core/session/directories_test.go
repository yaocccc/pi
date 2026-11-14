package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func directorySession(t *testing.T, root, name, cwd string, modified time.Time) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"type": "session", "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "sessions", name+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecentDirectoriesOrderLimitAndDeduplicate(t *testing.T) {
	root := t.TempDir()
	base := time.Unix(1700000000, 0)
	dirs := make([]string, 12)
	for i := range dirs {
		dirs[i] = filepath.Join(t.TempDir(), fmt.Sprintf("project-%02d", i))
		if err := os.Mkdir(dirs[i], 0700); err != nil {
			t.Fatal(err)
		}
		directorySession(t, root, fmt.Sprintf("bucket/%02d", len(dirs)-i), dirs[i], base.Add(time.Duration(i)*time.Second))
	}
	// A newer session for the first cwd wins; an older spelling of the last
	// cwd must neither duplicate it nor replace its more recent timestamp.
	directorySession(t, root, "new-first", dirs[0], base.Add(20*time.Second))
	directorySession(t, root, "old-last", dirs[11]+string(filepath.Separator)+".", base.Add(-time.Hour))
	want := []string{dirs[0]}
	for i := len(dirs) - 1; i > 0; i-- {
		want = append(want, dirs[i])
	}
	for _, limit := range []int{1, 10, 100} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			got, err := RecentDirectories(root, limit)
			if err != nil {
				t.Fatal(err)
			}
			if expected := want[:min(limit, len(want))]; !reflect.DeepEqual(got, expected) {
				t.Fatalf("got %v, want %v", got, expected)
			}
		})
	}
}

func TestRecentDirectoriesTieOrder(t *testing.T) {
	root, projects := t.TempDir(), t.TempDir()
	when := time.Unix(1700000000, 123456789)
	want := []string{filepath.Join(projects, "a"), filepath.Join(projects, "b")}
	for i, cwd := range want {
		if err := os.Mkdir(cwd, 0700); err != nil {
			t.Fatal(err)
		}
		// Session filename ordering deliberately opposes cwd ordering.
		directorySession(t, root, fmt.Sprint(2-i), cwd, when)
	}
	got, err := RecentDirectories(root, 10)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestRecentDirectoriesSkipInvalidHeadersAndDirectories(t *testing.T) {
	root, cwd, invalidCwd := t.TempDir(), t.TempDir(), t.TempDir()
	when := time.Unix(1700000000, 0)
	valid := directorySession(t, root, "valid", cwd, when)
	// A final header without a newline is valid too.
	raw, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(valid, raw[:len(raw)-1], 0600); err != nil {
		t.Fatal(err)
	}
	directorySession(t, root, "missing", filepath.Join(root, "deleted-project"), when)
	file := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	directorySession(t, root, "file-cwd", file, when)
	directorySession(t, root, "empty-cwd", "", when)
	directorySession(t, root, "relative-cwd", ".", when)
	for name, header := range map[string]string{
		"malformed":  "{broken json\n",
		"wrong-type": `{"type":"message","cwd":` + fmt.Sprintf("%q", invalidCwd) + "}\n",
		"oversized":  `{"type":"session","cwd":` + fmt.Sprintf("%q", invalidCwd) + `,"padding":"` + strings.Repeat("x", 70*1024) + "\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "sessions", name+".jsonl"), []byte(header), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := RecentDirectories(root, 10)
	if err != nil || !reflect.DeepEqual(got, []string{cwd}) {
		t.Fatalf("got %v, %v; want only %s", got, err, cwd)
	}
}

func TestRecentDirectoriesSkipSessionSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	cwd := t.TempDir()
	when := time.Unix(1700000000, 0)
	directorySession(t, root, "valid", cwd, when)
	external := directorySession(t, outside, "external", t.TempDir(), when.Add(time.Hour))
	for source, target := range map[string]string{
		external:               filepath.Join(root, "sessions", "linked.jsonl"),
		filepath.Dir(external): filepath.Join(root, "sessions", "linked-dir"),
	} {
		if err := os.Symlink(source, target); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	got, err := RecentDirectories(root, 10)
	if err != nil || !reflect.DeepEqual(got, []string{cwd}) {
		t.Fatalf("got %v, %v; symlinked sessions must be ignored", got, err)
	}
	linkedRoot := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "sessions"), filepath.Join(linkedRoot, "sessions")); err != nil {
		t.Fatal(err)
	}
	got, err = RecentDirectories(linkedRoot, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("symlinked sessions root: %v, %v", got, err)
	}
}

func TestRecentDirectoriesDoesNotScanMessageBodies(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := directorySession(t, root, "huge-body", cwd, time.Now())
	// A sparse malformed body larger than the normal session record limit
	// proves discovery does not parse or scan subsequent message records.
	if err := os.Truncate(path, 128*1024*1024); err != nil {
		t.Fatal(err)
	}
	got, err := RecentDirectories(root, 10)
	if err != nil || !reflect.DeepEqual(got, []string{cwd}) {
		t.Fatalf("got %v, %v; only the small header should be read", got, err)
	}
}

func TestRecentDirectoriesMissingAndInvalidRoot(t *testing.T) {
	root := t.TempDir()
	got, err := RecentDirectories(root, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("missing history: %v, %v", got, err)
	}
	file := filepath.Join(root, "agent-file")
	if err := os.WriteFile(file, []byte("not an agent directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecentDirectories(file, 10); err == nil {
		t.Fatal("unexpected traversal error was hidden")
	}
	for _, limit := range []int{0, -1} {
		got, err := RecentDirectories(file, limit)
		if err != nil || len(got) != 0 {
			t.Fatalf("limit %d should not traverse: %v, %v", limit, got, err)
		}
	}
}
