package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestUnfinishedFile(t *testing.T) {
	header := "{\"type\":\"session\",\"id\":\"s\"}\n"
	user := "{\"type\":\"message\",\"id\":\"u\",\"parentId\":null,\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"
	assistant := func(reason string) string {
		return fmt.Sprintf("{\"type\":\"message\",\"id\":\"a\",\"parentId\":\"u\",\"message\":{\"role\":\"assistant\",\"stopReason\":%q,\"content\":[]}}\n", reason)
	}
	for _, tt := range []struct {
		name, data      string
		unfinished, bad bool
	}{
		{"empty", "", false, false}, {"header", header, false, false},
		{"user", header + user, true, false},
		{"tool-use", header + user + assistant("toolUse"), true, false},
		{"unknown", header + user + assistant(""), true, false},
		{"stop", header + user + assistant("stop"), false, false},
		{"length", header + user + assistant("length"), false, false},
		{"error", header + user + assistant("error"), false, false},
		{"abort", header + user + assistant("aborted"), false, false},
		{"tool-result", header + user + assistant("toolUse") + `{"type":"message","id":"r","parentId":"a","message":{"role":"toolResult","content":[]}}`, true, false},
		{"metadata", header + user + `{"type":"session_info","id":"meta","parentId":"u","name":"Named"}`, true, false},
		{"partial-after-stop", header + user + assistant("stop") + `{"type":"message","id":"next`, false, true},
		{"branch", header + user + assistant("stop") + `{"type":"message","id":"branch","parentId":"u","message":{"role":"user","content":"branch"}}`, true, false},
		{"unrelated-old-branch", header + user + assistant("toolUse") + `{"type":"message","id":"branch","parentId":"u","message":{"role":"assistant","content":[],"stopReason":"stop"}}`, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := UnfinishedFile(path)
			if got != tt.unfinished || (err != nil) != tt.bad {
				t.Fatalf("got (%v,%v), want (%v,error=%v)", got, err, tt.unfinished, tt.bad)
			}
		})
	}
	if _, err := UnfinishedFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file treated as safe")
	}
}
