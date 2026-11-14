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

func TestReadPreviewRejectsMissingMalformedAndSymlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "history.jsonl")
	for _, contents := range []string{"", "not json\n", "{\"type\":\"session\"}\n", "{\"type\":\"message\",\"id\":\"not-header\"}\n"} {
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadPreview(path); err == nil {
			t.Fatalf("accepted invalid header: %q", contents)
		}
	}
	if _, _, err := ReadPreview(filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing file reported as empty history")
	}
	if _, _, err := ReadPreview(root); err == nil {
		t.Fatal("accepted a directory")
	}
	link := filepath.Join(root, "link.jsonl")
	if err := os.Symlink(path, link); err == nil {
		if _, _, err := ReadPreview(link); err == nil {
			t.Fatal("followed a symlink")
		}
	}
}
