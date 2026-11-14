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

const previewHeader = `{"type":"session","id":"preview","cwd":"/project"}` + "\n"

func TestPrepareFileMatchesEnvelopePreview(t *testing.T) {
	cases := map[string]string{
		"empty": "",
		"branch-and-metadata": strings.Join([]string{
			`{"type":"session_info","id":"name","name":"Saved name","parentId":null}`,
			`{"type":"message","id":"u","parentId":"name","message":{"role":"user","content":[{"type":"text","text":"before\n\nafter <tag>"},{"type":"image","data":"YWJj","mimeType":"image/png"}]}}`,
			`{"type":"message","id":"unused","parentId":"u","message":{"role":"assistant","content":"wrong branch"}}`,
			`{"type":"message","id":"call","parentId":"u","message":{"role":"assistant","content":[{"type":"thinking","thinking":"reason"},{"type":"toolCall","id":"t","name":"read","arguments":{"path":"x"}}]}}`,
			`{"type":"message","id":"result","parentId":"call","message":{"role":"toolResult","toolCallId":"t","toolName":"read","content":[{"type":"text","text":"result\n\nend"}],"details": { "unknown": [1, {"preserved":true}] }}}`,
			`{"type":"custom","id":"metadata","parentId":"result","customType":"future-extension","data": { "nested": ["kept"] }}`,
			`{"type":"compaction","id":"compact","parentId":"metadata","summary":"summary"}`,
			`{"type":"branch_summary","id":"branch","parentId":"compact","summary":"branch summary"}`,
			`{"type":"session","id":"ignored-header","parentId":42}`,
		}, "\n") + "\n",
		"truncated-tail":      `{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"saved"}}` + "\n" + `{"type":"message","id":"unfinished"`,
		"complete-no-newline": `{"type":"message","id":"u","parentId":null,"message":{"role":"user","content":"saved"}}`,
		"whitespace":          "\n \t\nnull\n",
	}
	for name, records := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.jsonl")
			contents := []byte(previewHeader + records)
			if err := os.WriteFile(path, contents, 0600); err != nil {
				t.Fatal(err)
			}
			base := State{File: path, ID: "existing", Busy: true, Steering: []string{"retained"}}
			header, raw, err := ReadPreview(path)
			if err != nil {
				t.Fatal(err)
			}
			old, err := base.SnapshotPreparation().Prepare(nil, raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			gotHeader, got, err := base.SnapshotPreparation().PrepareFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotHeader, header) {
				t.Fatal("header changed")
			}
			oldState, newState := base, base
			oldState.ApplySnapshot(old)
			newState.ApplySnapshot(got)
			a, _ := json.Marshal(oldState)
			b, _ := json.Marshal(newState)
			if string(a) != string(b) {
				t.Fatalf("projected state differs:\nold=%s\nnew=%s", a, b)
			}
			a, _ = json.Marshal(old.path)
			b, _ = json.Marshal(got.path)
			if string(a) != string(b) || old.state.leaf != got.state.leaf || !reflect.DeepEqual(old.state.pathIDs, got.state.pathIDs) {
				t.Fatal("active branch or metadata changed")
			}
			if len(old.state.records) != len(got.state.records) {
				t.Fatal("record count changed")
			}
			for i, r := range old.state.records {
				if r.key != got.state.records[i].key {
					t.Fatal("stable message key changed")
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(contents) {
				t.Fatal("preview changed file")
			}
			// Applying the reusable prepared result must detach mutable owner state.
			newState.aliases["local"] = "local"
			newState.pathIDs["local"] = true
			var other State
			other.ApplySnapshot(got)
			if other.aliases["local"] != "" || other.pathIDs["local"] {
				t.Fatal("prepared result leaked owner mutations")
			}
		})
	}
}

func TestPrepareFileRejectsInvalidHistory(t *testing.T) {
	for name, contents := range map[string]string{
		"empty": "", "missing-header": `{"type":"message","id":"x"}`,
		"header-id":          `{"type":"session"}`,
		"malformed-record":   previewHeader + `{"type":"message","id":"x",broken}`,
		"newline-truncation": previewHeader + `{"type":"message","id":"x"` + "\n",
		"invalid-schema":     previewHeader + `{"type":"message","id":"x","parentId":12,"message":{}}`,
		"duplicate":          previewHeader + "{\"type\":\"message\",\"id\":\"x\"}\n{\"type\":\"message\",\"id\":\"x\"}\n",
		"missing-parent":     previewHeader + `{"type":"message","id":"x","parentId":"missing","message":{"role":"user","content":"x"}}`,
		"cycle":              previewHeader + `{"type":"message","id":"x","parentId":"x","message":{"role":"user","content":"x"}}`,
		"malformed-message":  previewHeader + `{"type":"message","id":"x","message":12}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.jsonl")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			var s State
			if _, prepared, err := s.SnapshotPreparation().PrepareFile(path); err == nil || prepared != nil {
				t.Fatal("invalid file yielded a snapshot")
			}
		})
	}
	var s State
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "missing")} {
		if _, _, err := s.SnapshotPreparation().PrepareFile(path); err == nil {
			t.Fatal("invalid path accepted")
		}
	}
	path := filepath.Join(dir, "history.jsonl")
	if err := os.WriteFile(path, []byte(previewHeader), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(path, link); err == nil {
		if _, _, err := s.SnapshotPreparation().PrepareFile(link); err == nil {
			t.Fatal("followed symlink")
		}
	}
}

func TestPrepareFileRejectsOversizedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(previewHeader); err != nil {
		f.Close()
		t.Fatal(err)
	}
	// A sparse record exercises the shared reader limit without retaining a
	// second 64 MiB fixture string in the test process.
	err = f.Truncate(int64(len(previewHeader)) + 64*1024*1024 + 1)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	var state State
	_, prepared, err := state.SnapshotPreparation().PrepareFile(path)
	if err == nil || !strings.Contains(err.Error(), "record exceeds 64 MiB") || prepared != nil {
		t.Fatalf("oversized record: snapshot=%v err=%v", prepared != nil, err)
	}
}

func TestPreviewDescriptorRejectsConcurrentChanges(t *testing.T) {
	for _, change := range []string{"append", "replace", "modified"} {
		t.Run(change, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.jsonl")
			if err := os.WriteFile(path, []byte(previewHeader), 0600); err != nil {
				t.Fatal(err)
			}
			_, f, _, opened, err := openPreview(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			switch change {
			case "append":
				if err := os.WriteFile(path, []byte(previewHeader+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "replace":
				replacement := path + ".replacement"
				if err := os.WriteFile(replacement, []byte(previewHeader), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Skipf("cannot replace open file: %v", err)
				}
			case "modified":
				later := opened.ModTime().Add(time.Second)
				if err := os.Chtimes(path, later, later); err != nil {
					t.Fatal(err)
				}
			}
			if err := checkPreviewFile(path, opened); err == nil {
				t.Fatal("accepted changed file")
			}
		})
	}
}

func BenchmarkPrepareFileLargeHistory(b *testing.B) {
	path := filepath.Join(b.TempDir(), "large.jsonl")
	var file strings.Builder
	file.WriteString(previewHeader)
	text := strings.Repeat("historical content <tag>\n", 512)
	for i := 0; i < 800; i++ {
		parent := "null"
		if i > 0 {
			parent = fmt.Sprintf("%q", fmt.Sprintf("m%d", i-1))
		}
		body, _ := json.Marshal(map[string]any{"role": "user", "content": text})
		fmt.Fprintf(&file, "{\"type\":\"message\",\"id\":\"m%d\",\"parentId\":%s,\"message\":%s}\n", i, parent, body)
	}
	if err := os.WriteFile(path, []byte(file.String()), 0600); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"envelope", "native"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(file.Len()))
			for i := 0; i < b.N; i++ {
				var state State
				var prepared *PreparedSnapshot
				var err error
				if mode == "native" {
					_, prepared, err = state.SnapshotPreparation().PrepareFile(path)
				} else {
					var raw json.RawMessage
					_, raw, err = ReadPreview(path)
					if err == nil {
						prepared, err = state.SnapshotPreparation().Prepare(nil, raw, nil)
					}
				}
				if err != nil {
					b.Fatal(err)
				}
				state.ApplySnapshot(prepared)
				if state.MessageCount != 800 {
					b.Fatal("lost history")
				}
			}
		})
	}
}
