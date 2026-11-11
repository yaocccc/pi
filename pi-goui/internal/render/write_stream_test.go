package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestWriteStreamingPreview(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(fmt.Sprintf("snapshot=%t", snapshot), func(t *testing.T) {
			var state session.State
			var cache TranscriptCache
			opts := Options{Width: 100, ExpandTools: true, Expanded: map[string]bool{}}
			apply := func(v any) { raw, _ := json.Marshal(v); state.Apply(raw) }
			event := func(kind string, fields map[string]any) {
				fields["type"], fields["contentIndex"] = kind, 0
				apply(map[string]any{"type": "message_update", "assistantMessageEvent": fields})
			}
			apply(map[string]any{"type": "agent_start"})
			event("toolcall_start", map[string]any{"id": "stream", "toolName": "write"})
			render := func() string {
				t.Helper()
				return ansi.Strip(assertCachedTranscript(t, &cache, state.Items, testTheme(), opts).Text)
			}
			mustContainVisual(t, render(), "writing (awaiting path)", "╭─ write")
			source := ""
			for i := 1; i <= 15; i++ {
				line := fmt.Sprintf("stream-line-%02d\n", i)
				source += line
				if snapshot {
					apply(map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "stream", "name": "write", "arguments": map[string]any{"path": "stream.txt", "content": source}}}}})
				} else {
					encoded, _ := json.Marshal(line)
					delta := string(encoded[1 : len(encoded)-1])
					if i == 1 {
						delta = `{"path":"stream.txt","content":"` + delta
					}
					event("toolcall_delta", map[string]any{"delta": delta})
				}
				text := render()
				mustContainVisual(t, text, "writing stream.txt", fmt.Sprintf("%d │ stream-line-%02d", i, i))
				if strings.Contains(text, "written") {
					t.Fatal("preview marked written")
				}
				if i > 10 && strings.Contains(text, "stream-line-01") {
					t.Fatal("stream head not folded")
				}
			}
			mustContainVisual(t, render(), "5 earlier lines hidden", "6 │ stream-line-06")
			event("toolcall_end", map[string]any{"toolCall": map[string]any{"type": "toolCall", "id": "stream", "name": "write", "arguments": map[string]any{"path": "final.txt", "content": "final authoritative content"}}})
			mustContainVisual(t, render(), "writing final.txt", "final authoritative content")
			apply(map[string]any{"type": "tool_execution_start", "toolCallId": "stream", "toolName": "write", "args": map[string]string{"path": "final.txt", "content": "final authoritative content"}})
			mustContainVisual(t, render(), "writing final.txt")
			apply(map[string]any{"type": "tool_execution_end", "toolCallId": "stream", "toolName": "write", "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "success"}}}})
			text := render()
			mustContainVisual(t, text, "written final.txt")
			if strings.Contains(text, "writing ") {
				t.Fatal("completed write still writing")
			}
		})
	}
}
