package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mutationFixture(t *testing.T) (string, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.jsonl")
	original := []byte(`{"type":"session","version":3,"id":"test-id","cwd":"/tmp"}` + "\n" + `{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":[{"type":"text","text":"hello"}]}}` + "\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	return path, original
}
func TestMutationRenameAppendsNativeSessionInfoWithoutRewriting(t *testing.T) {
	path, before := mutationFixture(t)
	file, err := InspectMutationFile(path, "test-id")
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Rename("  新名称\nnext  "); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("history bytes rewritten")
	}
	var entry map[string]any
	if err = json.Unmarshal(after[len(before):], &entry); err != nil {
		t.Fatal(err)
	}
	if entry["type"] != "session_info" || entry["parentId"] != "root" || entry["name"] != "新名称 next" || len(entry["id"].(string)) != 8 {
		t.Fatalf("invalid native entry: %v", entry)
	}
	read, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		LeafID string `json:"leafId"`
	}
	_ = json.Unmarshal(read, &decoded)
	if decoded.LeafID != entry["id"] {
		t.Fatal("new metadata is not native leaf")
	}
	file, err = InspectMutationFile(path, "test-id")
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Rename(""); err != nil {
		t.Fatal(err)
	}
}
func TestMutationRejectsInvalidReplacedAndSymlinkFiles(t *testing.T) {
	for _, content := range []string{"", `{"type":"session","version":3,"id":"test-id"}`, "{}\n", `{"type":"session","version":2,"id":"test-id"}` + "\n", `{"type":"session","version":3,"id":"test-id"}` + "\n" + `{"type":"message","id":"one","parentId":"missing"}` + "\n"} {
		p := filepath.Join(t.TempDir(), "bad.jsonl")
		_ = os.WriteFile(p, []byte(content), 0600)
		if _, err := InspectMutationFile(p, ""); err == nil {
			t.Errorf("accepted invalid file %q", content)
		}
		got, _ := os.ReadFile(p)
		if string(got) != content {
			t.Fatal("validation changed file")
		}
	}
	path, _ := mutationFixture(t)
	if _, err := InspectMutationFile(path, "wrong-id"); err == nil {
		t.Fatal("accepted wrong selected ID")
	}
	link := filepath.Join(t.TempDir(), "link.jsonl")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectMutationFile(link, ""); err == nil {
		t.Fatal("followed symlink")
	}
	file, _ := InspectMutationFile(path, "test-id")
	replacement, _ := mutationFixture(t)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := file.Delete(); err == nil {
		t.Fatal("deleted replaced inode")
	}
	if err := file.Rename("bad"); err == nil {
		t.Fatal("renamed replaced inode")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("replacement disappeared")
	}
}
func TestMutationRechecksContentAndDeletesOnlySelectedFile(t *testing.T) {
	path, _ := mutationFixture(t)
	file, _ := InspectMutationFile(path, "test-id")
	handle, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = handle.WriteString(`{"type":"session_info","id":"info","parentId":"root","name":"other"}` + "\n")
	_ = handle.Close()
	if err := file.Delete(); err == nil {
		t.Fatal("deleted concurrently changed history")
	}
	if err := file.Rename("bad"); err == nil {
		t.Fatal("appended to concurrently changed history")
	}
	next, err := InspectMutationFile(path, "test-id")
	if err != nil {
		t.Fatal(err)
	}
	if !file.SameFile(next) {
		t.Fatal("append incorrectly considered replacement")
	}
	sibling := filepath.Join(filepath.Dir(path), "other.jsonl")
	_ = os.WriteFile(sibling, []byte("untouched"), 0600)
	if err = next.Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("selected file remains")
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "untouched" {
		t.Fatal("other file changed")
	}
}
