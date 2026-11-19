package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A change to canonical keys would affect stable aliases, refresh and fork
// checkpoints. Compare the optimized encoding to the original two stages.
func TestPreviewCanonicalKeyCompatibility(t *testing.T) {
	legacyKey := func(m message) string {
		var parts []content
		if m.stream != nil {
			parts = m.stream
		} else {
			var text string
			if json.Unmarshal(m.Content, &text) == nil {
				parts = []content{{Type: "text", Text: text}}
			} else {
				_ = json.Unmarshal(m.Content, &parts)
			}
		}
		m.Content, _ = json.Marshal(parts)
		b, _ := json.Marshal(messageJSON(m))
		return fmt.Sprintf("%s:%x", m.Role, sha256.Sum256(b))
	}
	shown, exit := true, 7
	base := message{Role: "custom", CustomType: "x", Details: json.RawMessage(`{"key":"x"}`), Timestamp: json.RawMessage("123"), ToolCallID: "id", ToolName: "tool", IsError: &shown, StopReason: "stop", ErrorMessage: "Error", Command: "cmd", Output: "out", Summary: "summary", ExitCode: &exit, Display: &shown}
	var cases []message
	for _, raw := range []string{"", " \n null", "[]", `"inline \u003c&"`, ` [{"type":"text","text":"a"},{"type":"image","mimeType":"image/png","data":"aBc=="},{"type":"toolCall","arguments":{"op":"<"}}]`, `{"not":"blocks"}`, `[{"type":"text","text":0}]`} {
		m := base
		m.Content = json.RawMessage(raw)
		cases = append(cases, m)
	}
	for _, args := range []string{`{ "a":"&" }`, `{"unfinished":`} {
		m := base
		m.stream = []content{{Type: "toolCall", Arguments: json.RawMessage(args)}}
		cases = append(cases, m)
	}
	for i, m := range cases {
		if got, want := keyOf(m), legacyKey(m); got != want {
			t.Fatalf("case %d: changed message key", i)
		}
	}
}

// Alternating native turns with a sizable tool payload, not only large user images.
func BenchmarkPrepareFileToolTurns(b *testing.B) {
	file := filepath.Join(b.TempDir(), "tool-turns.jsonl")
	f, err := os.Create(file)
	if err != nil {
		b.Fatal(err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, `{"type":"session","id":"large","version":3}`)
	payload := strings.Repeat("read output 中文: abcde\n", 512)
	parent := "null"
	for i := range 1024 {
		for j, m := range []string{
			`{"role":"user","content":"review the file"}`,
			fmt.Sprintf(`{"role":"assistant","content":[{"type":"toolCall","name":"read","id":"t%d","arguments":{"path":"example"}}]}`, i),
			fmt.Sprintf(`{"role":"toolResult","toolCallId":"t%d","content":[{"type":"text","text":%q}],"details":{"truncated":false}}`, i, payload),
		} {
			id := fmt.Sprintf("e%d-%d", i, j)
			fmt.Fprintf(w, "{\"type\":\"message\",\"id\":%q,\"parentId\":%s,\"message\":%s}\n", id, parent, m)
			parent = fmt.Sprintf("%q", id)
		}
	}
	if err := w.Flush(); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	stat, err := os.Stat(file)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(stat.Size())
	b.ReportAllocs()
	prep := (&State{}).SnapshotPreparation()
	b.ResetTimer()
	for range b.N {
		_, p, err := prep.PrepareFile(file)
		if err != nil {
			b.Fatal(err)
		}
		if p.state.MessageCount != 3072 || len(p.state.records[len(p.state.records)-1].msg.Content) < len(payload) {
			b.Fatalf("missing history: count=%d", p.state.MessageCount)
		}
	}
}
