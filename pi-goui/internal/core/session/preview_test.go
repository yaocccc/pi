package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPreviewValidatesHeaderAndPreservesNativeBranch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "history.jsonl")
	contents := `{"type":"session","id":"saved","cwd":"/project"}` + "\n" +
		`{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"history"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	header, raw, err := ReadPreview(path)
	if err != nil || header.ID != "saved" || header.Cwd != "/project" || !strings.Contains(string(raw), `"leafId":"u"`) {
		t.Fatalf("preview: %#v %s %v", header, raw, err)
	}
	want, _ := ReadFile(path)
	if string(want) != string(raw) {
		t.Fatal("preview diverged from native file projection")
	}
	after, _ := os.ReadFile(path)
	if string(after) != contents {
		t.Fatal("preview wrote to history")
	}
}

func TestPreviewRejectsInvalidHeadersAndPaths(t *testing.T) {
	root := t.TempDir()
	reject := func(t *testing.T, path string) {
		t.Helper()
		if _, _, err := ReadPreview(path); err == nil {
			t.Errorf("ReadPreview accepted %q", path)
		}
		var state State
		if _, prepared, err := state.SnapshotPreparation().PrepareFile(path); err == nil || prepared != nil {
			t.Errorf("PrepareFile returned a snapshot or no error for %q", path)
		}
	}
	for name, contents := range map[string]string{
		"empty":             "",
		"malformed":         "not json\n",
		"missing-id":        `{"type":"session"}` + "\n",
		"wrong-type":        `{"type":"message","id":"not-header"}` + "\n",
		"unterminated-id":   `{"type":"session"}`,
		"unterminated-type": `{"type":"message","id":"x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			reject(t, writeSessionFixture(t, root, name, []byte(contents)))
		})
	}
	t.Run("missing", func(t *testing.T) { reject(t, filepath.Join(root, "missing")) })
	t.Run("directory", func(t *testing.T) { reject(t, root) })
	t.Run("symlink", func(t *testing.T) {
		target := writeSessionFixture(t, root, "valid", []byte(previewHeader))
		link := filepath.Join(root, "link.jsonl")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		reject(t, link)
	})
}
