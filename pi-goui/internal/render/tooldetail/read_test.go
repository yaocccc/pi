package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func TestReadDetailContentHierarchy(t *testing.T) {
	source := "第一行\n\n末行🙂\n"
	args, _ := json.Marshal(map[string]any{"path": "目录/文件.go", "offset": 7})
	details, _ := json.Marshal(map[string]any{"truncation": map[string]any{"content": source, "truncated": true}, "future": "KEEP"})
	item := session.Item{ToolName: "read", Arguments: args, Details: details, Text: source + "\n[Showing lines 7–9. Use offset=10 to continue.]", HasResult: true}
	for _, width := range []int{1, 20, 60} {
		hooks := testHooks(t)
		var actualSource string
		hooks.CodeRows = func(code, path, language string, start, w int, theme config.Theme) []string {
			actualSource = code
			if path != "目录/文件.go" || start != 7 || w != width {
				t.Fatalf("wrong code coordinates: %q %d %d", path, start, w)
			}
			return []string{code}
		}
		text := strings.Join(Render(item, config.Theme{}, width, hooks), "\n")
		for _, want := range []string{"File · 目录/文件.go · from line 7", "Content", "Lines 7–9", "Read notice", "Use offset=10", "future: KEEP"} {
			if !strings.Contains(text, want) {
				t.Fatalf("missing %q: %s", want, text)
			}
		}
		if actualSource != source || strings.Count(text, "第一行") != 1 {
			t.Fatalf("source altered or duplicated: %s", text)
		}
	}
}

func TestReadDetailImageMatchesTranscript(t *testing.T) {
	for _, source := range []string{"[image]", "Read image file [image/png]"} {
		hooks := testHooks(t)
		hooks.CodeRows = func(string, string, string, int, int, config.Theme) []string {
			t.Fatal("image attachment was rendered as numbered source")
			return nil
		}
		text := strings.Join(Render(session.Item{ToolName: "read", Text: source, HasResult: true, Details: json.RawMessage(`{"future":"KEEP"}`)}, config.Theme{}, 30, hooks), "\n")
		if !strings.Contains(text, "Image attachment") || !strings.Contains(text, source) || !strings.Contains(text, "future: KEEP") {
			t.Fatal(text)
		}
	}
}

func TestReadDetailEmptyResultVersusPending(t *testing.T) {
	item := session.Item{ToolName: "read"}
	pending := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	item.HasResult = true
	complete := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	if !strings.Contains(pending, "waiting for result") || !strings.Contains(complete, "empty result") || strings.Contains(complete, "waiting") {
		t.Fatalf("pending=%s complete=%s", pending, complete)
	}
}
