package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		{"replaced-message", header + user + `{"type":"message","id":"a","parentId":"u","message":{"role":"assistant","stopReason":"stop"},"message":{"role":"assistant"}}`, true, false},
		{"cleared-message", header + user + `{"type":"message","id":"a","parentId":"u","message":{"role":"assistant","stopReason":"stop"},"message":null}`, true, false},
		{"metadata-after-stop", header + user + assistant("stop") + `{"type":"session_info","id":"meta","parentId":"a","name":"Named"}`, false, false},
		{"custom-message-after-user", header + user + `{"type":"message","id":"custom","parentId":"u","message":{"role":"custom","content":"notice"}}`, true, false},
		{"non-message-role", header + user + `{"type":"custom","id":"custom","parentId":"u","message":{"role":"assistant","stopReason":"stop"}}`, true, false},
		{"missing-ancestor", header + assistant("stop"), false, true},
		{"duplicate-entry", header + user + assistant("stop") + assistant("stop"), false, true},
		{"cycle-before-stop", header + strings.Replace(user, "null", `"a"`, 1) + assistant("stop"), false, true},
		{"off-branch-cycle", header + user + `{"type":"custom","id":"x","parentId":"x"}` + "\n" + assistant("stop"), false, false},
		{"malformed-earlier-record", header + "{bad}\n" + assistant("stop"), false, true},
		{"partial-with-newline", header + user + assistant("stop") + "{\"type\":\n", false, true},
		{"trailing-whitespace", header + user + assistant("stop") + " \n\t\n ", false, false},
		{"complete-without-newline", header + user + strings.TrimSuffix(assistant("stop"), "\n"), false, false},
		{"escaped-completion", header + user + `{"type":"message","id":"a","parentId":"u","message":{"role":"\u0061ssistant","stopReason":"\u0073top"}}`, false, false},
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

func BenchmarkUnfinishedFile(b *testing.B) {
	data := fmt.Sprintf("{\"type\":\"session\",\"id\":\"s\"}\n"+
		"{\"type\":\"message\",\"id\":\"u\",\"message\":{\"role\":\"user\",\"content\":\"hi\"}}\n"+
		"{\"type\":\"message\",\"id\":\"a\",\"parentId\":\"u\",\"message\":{\"role\":\"assistant\",\"stopReason\":\"stop\",\"content\":\"%s\"}}\n",
		strings.Repeat("x", 1024*1024))
	path := filepath.Join(b.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if unfinished, err := UnfinishedFile(path); err != nil || unfinished {
			b.Fatalf("got unfinished=%t err=%v", unfinished, err)
		}
	}
}
