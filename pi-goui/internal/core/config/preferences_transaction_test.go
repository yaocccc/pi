package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each independent test process waits on the same start gate, then writes its
// own field exactly once. A later write cannot accidentally repair a lost key.
func TestConcurrentPreferenceProcesses(t *testing.T) {
	if k := os.Getenv("PI_TEST_PREF_KEY"); k != "" {
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		if err := savePreference(os.Getenv("PI_CODING_AGENT_DIR"), k, true); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	path := filepath.Join(dir, "goui.json")
	put(t, path, `{"unknown":"`+strings.Repeat("x", 1<<17)+`"}`)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	var children []*exec.Cmd
	defer func() {
		w.Close()
		for _, c := range children {
			if err := c.Wait(); err != nil {
				t.Error(err)
			}
		}
	}()
	for i := 0; i < 6; i++ {
		c := exec.Command(bin, "-test.run=^TestConcurrentPreferenceProcesses$")
		c.Dir = dir
		c.Env = append(os.Environ(), fmt.Sprintf("PI_TEST_PREF_KEY=writer%d", i))
		c.Stdin = r
		c.Stderr = os.Stderr
		c.Stdout = os.Stdout
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, c)
	}
	w.Close()
	for _, c := range children {
		if err := c.Wait(); err != nil {
			t.Error(err)
		}
	}
	n := len(children)
	children = nil
	var result map[string]json.RawMessage
	if err := readJSON(path, &result); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if string(result[fmt.Sprintf("writer%d", i)]) != "true" {
			t.Errorf("missing writer%d", i)
		}
	}
	if len(result["unknown"]) != (1<<17)+2 {
		t.Error("lost unknown field")
	}
}

func TestPreferenceSaveTimesOutBehindExistingLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goui.json")
	const before = `{"unknown":"retain"}`
	put(t, path, before)
	release, err := lockFrontend(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() { done <- SaveSidebarPosition(dir, "left") }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "lock") {
			t.Fatalf("expected lock acquisition error, got %v", err)
		}
	case <-time.After(frontendLockWait + 2*time.Second):
		t.Fatal("preference save waited indefinitely for the lock")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != before {
		t.Fatalf("contended save changed preferences: %s (%v)", data, err)
	}
}

func TestThinkingSaveHonorsNativeLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	put(t, path, `{"native":9007199254740993}`)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".lock", 0700); err != nil {
		t.Fatal(err)
	}
	if err := SaveDefaultThinkingLevel(dir, "high"); err == nil {
		t.Fatal("wrote settings despite native lock")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != `{"native":9007199254740993}` {
		t.Fatal("changed locked settings", err)
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := SaveDefaultThinkingLevel(dir, "high"); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := readJSON(path, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["native"]) != "9007199254740993" || string(fields["defaultThinkingLevel"]) != `"high"` {
		t.Fatal(fields)
	}
	after, err := os.Stat(path)
	if err != nil || before.Mode().Perm() != after.Mode().Perm() {
		t.Fatal("wrong mode", err)
	}
}
