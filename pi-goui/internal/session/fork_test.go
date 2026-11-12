package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func forkSource(t *testing.T) (string, json.RawMessage) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.jsonl")
	lines := []string{
		`{"type":"session","version":3,"id":"original","cwd":"/tmp"}`,
		`{"type":"model_change","id":"m","parentId":null,"provider":"test","modelId":"test-model"}`,
		`{"type":"thinking_level_change","id":"th","parentId":"m","thinkingLevel":"high"}`,
		`{"type":"session_info","id":"n","parentId":"th","name":"Original"}`,
		`{"type":"message","id":"u1","parentId":"n","message":{"role":"user","content":"first"}}`,
		`{"type":"message","id":"a1","parentId":"u1","message":{"role":"assistant","content":[{"type":"text","text":"valid earlier error"}],"stopReason":"error"}}`,
		`{"type":"label","id":"label","parentId":"a1","targetId":"a1","label":"keep"}`,
		`{"type":"custom","id":"custom","parentId":"label","customType":"fixture","data":{"keep":true}}`,
		`{"type":"compaction","id":"compact","parentId":"custom","summary":"summary","firstKeptEntryId":"u1","tokensBefore":100}`,
		`{"type":"message","id":"selected","parentId":"compact","message":{"role":"user","content":[{"type":"text","text":"edit me"},{"type":"image","data":"ignored"}]}}`,
		`{"type":"message","id":"error","parentId":"selected","message":{"role":"assistant","content":[{"type":"text","text":"This operation was aborted"}],"stopReason":"error"}}`,
		`{"type":"message","id":"other","parentId":"u1","message":{"role":"user","content":"other branch"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entries := make([]json.RawMessage, 0, len(lines)-1)
	for _, line := range lines[1:] {
		entries = append(entries, json.RawMessage(line))
	}
	// The RPC's selected leaf, deliberately not the physical last file entry.
	snapshot, _ := json.Marshal(map[string]any{"entries": entries, "leafId": "error"})
	return path, snapshot
}

func TestCreateForkPrefixAndIdentity(t *testing.T) {
	path, snapshot := forkSource(t)
	before, _ := os.ReadFile(path)
	fork, err := CreateFork(path, "original", "/tmp", "fork from Original", "selected", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if fork.ID == "original" || fork.Path == path || fork.Name != "fork from Original" {
		t.Fatalf("fork metadata: %+v", fork)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("source changed")
	}
	data, _ := os.ReadFile(fork.Path)
	if !strings.Contains(string(data), "This operation was aborted") || strings.Contains(string(data), "other branch") || !strings.Contains(string(data), "edit me") {
		t.Fatal("selected exchange missing or unrelated branch copied")
	}
	for _, keep := range []string{"valid earlier error", "thinking_level_change", "test-model", "compaction", "firstKeptEntryId", `"customType":"fixture"`, `"type":"label"`} {
		if !strings.Contains(string(data), keep) {
			t.Fatalf("lost prefix metadata %s", keep)
		}
	}
	var header struct {
		Version                int
		ID, Cwd, ParentSession string
	}
	if err := json.Unmarshal([]byte(strings.Split(string(data), "\n")[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.ID != fork.ID || header.Version != 3 || header.ParentSession != path || header.Cwd != "/tmp" {
		t.Fatalf("header: %+v", header)
	}
	if _, err := InspectMutationFile(fork.Path, fork.ID); err != nil {
		t.Fatal("invalid native file:", err)
	}
	raw, err := ReadFile(fork.Path)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := state.LoadEntries(raw); err != nil {
		t.Fatal(err)
	}
	if len(state.Items) == 0 || !strings.Contains(texts(&state), "aborted") {
		t.Fatal("bad projection")
	}
	second, err := CreateFork(path, "original", "/tmp", "fork from Original", "selected", snapshot)
	if err != nil || second.ID == fork.ID || second.Path == fork.Path {
		t.Fatal("fork not unique", err)
	}
}

func TestCreateForkFirstMessageAndRejections(t *testing.T) {
	path, snapshot := forkSource(t)
	fork, err := CreateFork(path, "original", "/tmp", "fork from Original", "u1", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fork.Entries), "first") || !strings.Contains(string(fork.Entries), "valid earlier error") || strings.Contains(string(fork.Entries), "edit me") || strings.Contains(string(fork.Entries), "This operation was aborted") {
		t.Fatal("first-message fork must include its reply but not the next exchange")
	}
	for _, id := range []string{"missing", "other", "error"} {
		if _, err := CreateFork(path, "original", "/tmp", "fork from Original", id, snapshot); err == nil {
			t.Fatal("accepted invalid selection", id)
		}
	}
	if _, err := CreateFork(path, "replacement", "/tmp", "name", "u1", snapshot); err == nil {
		t.Fatal("accepted changed identity")
	}
	broken := json.RawMessage(`{"entries":[{"id":"u","type":"message","parentId":"u","message":{"role":"user","content":"x"}}],"leafId":"u"}`)
	if _, err := CreateFork(path, "original", "/tmp", "name", "u", broken); err == nil {
		t.Fatal("accepted cycle")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.jsonl"))
	if len(files) != 2 {
		t.Fatalf("rejected forks wrote files: %v", files)
	}
}
