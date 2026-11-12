package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func TestWriteDetailPreviewAndCompletion(t *testing.T) {
	content := "中文🙂\n\n" + strings.Repeat("long source ", 100) + "END\n"
	args, _ := json.Marshal(map[string]any{"path": "长路径/source.go", "content": content})
	theme := config.Theme{Muted: "muted", Warning: "warning", Success: "success"}
	item := session.Item{ToolName: "write", Arguments: args, Details: json.RawMessage(`{"unknown":"PRESERVED"}`), Running: true}
	for _, done := range []bool{false, true} {
		item.Running = !done
		item.HasResult = done
		hooks := testHooks(t)
		var statusColor, gotSource string
		hooks.Line = func(text, color string, width int) []string {
			if strings.Contains(text, "长路径/source.go") {
				statusColor = color
			}
			return []string{text}
		}
		hooks.CodeRows = func(source, path, language string, start, width int, theme config.Theme) []string {
			gotSource = source
			return []string{source}
		}
		text := strings.Join(Render(item, theme, 20, hooks), "\n")
		if gotSource != content || !strings.Contains(text, "3 lines") || !strings.Contains(text, "Content") || !strings.Contains(text, "unknown: PRESERVED") {
			t.Fatal(text)
		}
		if done {
			if !strings.Contains(text, "✓ written 长路径/source.go") || strings.Contains(text, "preview") || statusColor != theme.Success {
				t.Fatal(text, statusColor)
			}
		} else if !strings.Contains(text, "◌ writing 长路径/source.go") || !strings.Contains(text, "preview") || statusColor != theme.Warning {
			t.Fatal(text, statusColor)
		}
	}
}

func TestWriteDetailGridWrapsWithoutTranscriptTailLimit(t *testing.T) {
	source := strings.Repeat("中文🙂 retained\n", 35) + "LAST"
	args, _ := json.Marshal(map[string]any{"path": "source.go", "content": source})
	for _, width := range []int{1, 9, 10, 20, 60} {
		for _, complete := range []bool{false, true} {
			hooks := testHooks(t)
			wantWidth := width
			if width >= 10 {
				wantWidth -= 2
			}
			hooks.CodeRows = func(got, path, language string, start, w int, theme config.Theme) []string {
				if got != source || path != "source.go" || start != 1 || w != wantWidth {
					t.Fatalf("grid dropped source or wrong gutter geometry: start=%d width=%d", start, w)
				}
				return []string{"source-first", "source-continuation", "source-last"}
			}
			railColor := ""
			hooks.Line = func(text, color string, w int) []string {
				if text == "▌ " {
					railColor = color
					if w != 2 {
						t.Fatal("rail width", w)
					}
				}
				return []string{text}
			}
			theme := config.Theme{Warning: "pending", Success: "done"}
			text := strings.Join(Render(session.Item{ToolName: "write", Arguments: args, HasResult: complete, Running: !complete}, theme, width, hooks), "\n")
			if width >= 10 {
				for _, row := range []string{"source-first", "source-continuation", "source-last"} {
					if !strings.Contains(text, "▌ "+row) {
						t.Fatal("missing continuation rail", text)
					}
				}
				wantColor := theme.Warning
				if complete {
					wantColor = theme.Success
				}
				if railColor != wantColor {
					t.Fatalf("rail color %q, want %q", railColor, wantColor)
				}
			} else if strings.Contains(text, "▌ ") {
				t.Fatal("narrow viewport spent source cells on rail")
			}
		}
	}
}

func TestWriteDetailEmptyAndMissingContent(t *testing.T) {
	for _, tt := range []struct{ args, want string }{{`{"content":""}`, "0 lines · 0 B"}, {`{}`, "waiting for content"}} {
		text := strings.Join(Render(session.Item{ToolName: "write", Arguments: json.RawMessage(tt.args)}, config.Theme{}, 40, testHooks(t)), "\n")
		if !strings.Contains(text, tt.want) {
			t.Fatal(text)
		}
	}
}
