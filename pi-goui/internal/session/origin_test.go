package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestForkProvenanceRoundTripAndNested(t *testing.T) {
	path, snapshot := forkSource(t)
	preview, err := PreviewFork("selected", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	fork, err := CreateFork(path, "original", "/tmp", "branch", "selected", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	h, err := ReadHeader(fork.Path, fork.ID)
	if err != nil || !reflect.DeepEqual(h, fork.Origin) {
		t.Fatalf("header roundtrip: %+v %v", h, err)
	}
	want := &ForkProvenance{Version: 1, SourceID: "original", UserEntryID: "selected", CutoffEntryID: preview.LeafID, Turn: preview.Turn}
	if h.ParentSession != path || !reflect.DeepEqual(h.Fork, want) {
		t.Fatalf("provenance: %+v", h)
	}
	if strings.Contains(string(fork.Entries), "piGouiFork") {
		t.Fatal("UI metadata leaked into model entries")
	}
	nested, err := CreateFork(fork.Path, fork.ID, "/tmp", "nested", "selected", fork.Entries)
	if err != nil {
		t.Fatal(err)
	}
	nh, err := ReadHeader(nested.Path, nested.ID)
	if err != nil || nh.ParentSession != fork.Path || nh.Fork.SourceID != fork.ID {
		t.Fatalf("not direct parent: %+v %v", nh, err)
	}
	// Native append-only name changes retain UI metadata in the header.
	f, err := InspectMutationFile(fork.Path, fork.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Rename("renamed"); err != nil {
		t.Fatal(err)
	}
	renamed, err := ReadHeader(fork.Path, fork.ID)
	if err != nil || !reflect.DeepEqual(renamed, h) {
		t.Fatalf("rename lost origin: %+v %v", renamed, err)
	}
}

func TestOriginLegacyListAndSafety(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "cwd")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "parent.jsonl")
	write := func(path string, h Header) {
		t.Helper()
		raw, _ := json.Marshal(h)
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(parent, Header{Type: "session", ID: "parent", Cwd: "/tmp"})
	child := filepath.Join(dir, "legacy.jsonl")
	write(child, Header{Type: "session", ID: "child", Cwd: "/tmp", ParentSession: parent})
	h, err := ReadHeader(child, "child")
	if err != nil || h.ParentSession != parent || h.Fork != nil {
		t.Fatalf("legacy: %+v %v", h, err)
	}
	list, err := List(root, "")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	found := false
	for _, s := range list {
		if s.ID == "child" {
			found = s.ParentSession == parent && s.Fork == nil
		}
	}
	if !found {
		t.Fatal("List lost legacy parent")
	}
	if _, err = ReadHeader(child, "replacement"); err == nil {
		t.Fatal("ID mismatch accepted")
	}
	link := filepath.Join(dir, "link.jsonl")
	if err = os.Symlink(child, link); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadHeader(link, "child"); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err = ReadHeader(filepath.Join(dir, "missing.jsonl"), ""); err == nil {
		t.Fatal("missing accepted")
	}
	write(child, Header{Type: "session", ID: "child", ParentSession: parent, Fork: &ForkProvenance{Version: 99, SourceID: "parent", UserEntryID: "u", CutoffEntryID: "a", Turn: 1}})
	h, err = ReadHeader(child, "child")
	if err != nil || h.Fork != nil || h.ParentSession != parent {
		t.Fatal("unknown provenance should degrade to native parent", h, err)
	}
}
