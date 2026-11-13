package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func TestCommandStatusAndExitColors(t *testing.T) {
	theme := config.Theme{Success: "success", Error: "error", Muted: "muted"}
	for _, tc := range []struct{ exit, color string }{{"0", "success"}, {"2", "error"}, {"null", "muted"}} {
		hooks := testHooks(t)
		seen := false
		hooks.Line = func(text, color string, width int) []string {
			if strings.HasSuffix(text, "Exit code · "+tc.exit) || tc.exit == "null" && strings.HasSuffix(text, "Exit code · —") {
				seen = color == tc.color
			}
			return strings.Split(text, "\n")
		}
		item := session.Item{ToolName: "bash", HasResult: true, Arguments: json.RawMessage(`{"command":"printf output"}`), Details: json.RawMessage(`{"exitCode":` + tc.exit + `,"future":"kept"}`), Text: "first\n\n  last  "}
		text := strings.Join(Render(item, theme, 30, hooks), "\n")
		if !seen || !strings.HasPrefix(text, "$ Command\n") || !strings.Contains(text, "■ Finished") || !strings.Contains(text, "Output\nfirst\n\n  last  ") || !strings.Contains(text, "future: kept") {
			t.Fatal(text)
		}
	}
}

func TestExecutionDetailsWidthAndFullContent(t *testing.T) {
	long := strings.Repeat("中文🙂result ", 25) + "END"
	for _, name := range []string{"bash", "shell", "powershell", "codemode"} {
		for _, width := range []int{20, 30, 40, 60} {
			hooks := testHooks(t)
			hooks.Line = func(text, _ string, width int) []string {
				return strings.Split(ansi.Hardwrap(text, width, true), "\n")
			}
			hooks.CodeRows = func(source, path, language string, start, width int, theme config.Theme) []string {
				return strings.Split(ansi.Hardwrap(source, width, true), "\n")
			}
			args, _ := json.Marshal(map[string]any{"command": long, "code": long, "timeout": 10, "cwd": "中文目录", "future": "FUTURE"})
			item := session.Item{ToolName: name, HasResult: true, Arguments: args, Text: long + "\n\n  REAL BLANK LINE\n", Details: json.RawMessage(`{"futureDetails":"DETAIL_END"}`)}
			rows := Render(item, config.Theme{}, width, hooks)
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("%s width %d: %q", name, width, row)
				}
			}
			text := strings.Join(rows, "\n")
			if !strings.Contains(text, "\n\n  REAL BLANK LINE\n") || !strings.Contains(strings.Join(rows, ""), strings.ReplaceAll(long, "\n", "")) || !strings.Contains(text, "FUTURE") || !strings.Contains(strings.Join(rows, ""), "DETAIL_END") {
				t.Fatalf("%s width %d lost content: %s", name, width, text)
			}
		}
	}
}

func TestCommandStreamingAndFailure(t *testing.T) {
	item := session.Item{ToolName: "powershell", Running: true, Arguments: json.RawMessage(`{"script":"Get-Location"}`), Text: "partial"}
	text := strings.Join(Render(item, config.Theme{}, 20, testHooks(t)), "\n")
	if !strings.Contains(text, "Running") || strings.Contains(text, "Finished") || !strings.Contains(text, "code::powershell:1") {
		t.Fatal(text)
	}
	item.Error, item.HasResult, item.Running = true, true, false
	item.Details = json.RawMessage(`{"exitCode":7}`)
	text = strings.Join(Render(item, config.Theme{}, 20, testHooks(t)), "\n")
	if !strings.HasPrefix(text, "Tool failed\npartial") || strings.Contains(text, "Finished") || !strings.Contains(text, "✗ Exit code · 7") || !strings.Contains(text, "code::powershell:1") {
		t.Fatal(text)
	}
}
