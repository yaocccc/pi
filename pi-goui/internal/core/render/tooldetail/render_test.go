package tooldetail

import (
	"encoding/json"
	"fmt"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/extensions/toolrender/detail"
	"reflect"
	"strings"
	"testing"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

// Stub only terminal primitives; dispatch and tool semantics run in this package.
// Parent render tests exercise the real highlighting, wrapping and ANSI hooks.
func testHooks(t *testing.T) Hooks {
	t.Helper()
	return Hooks{
		Line: func(text, color string, width int) []string {
			if width < 1 {
				t.Fatal("nonpositive rendering width")
			}
			return strings.Split(text, "\n")
		},
		Clean: func(s string) string { return s },
		CodeRows: func(source, path, language string, start, width int, theme config.Theme) []string {
			return []string{fmt.Sprintf("code:%s:%s:%d", path, language, start), source}
		},
		DiffRows:   func(diff string, width int, theme config.Theme) []string { return []string{diff} },
		DiffCounts: func(string) (int, int) { return 1, 1 },
	}
}

func TestDedicatedDispatchAndAliases(t *testing.T) {
	provider := func(host extensionapi.DetailHost, name string, item session.Item, args, details map[string]any) bool {
		return detail.New(host).Render(name, item, args, details)
	}

	for _, tt := range []struct {
		name, args, details, text, want string
	}{
		{"functions.READ", `{"path":"a.go","offset":7}`, `{}`, "source", "code:a.go::7"},
		{"write", `{"path":"b.go","content":"saved source"}`, `{}`, "", "written b.go"},
		{"edit", `{"path":"c.go"}`, `{"diff":"-old\n+new"}`, "", "Diff · +1 −1"},
		{"bash", `{"command":"pwd"}`, `{"exitCode":0}`, "output", "code::bash:1"},
		{"shell", `{"cmd":"pwd"}`, `{}`, "output", "code::bash:1"},
		{"powershell", `{"script":"Get-Location"}`, `{}`, "output", "code::powershell:1"},
		{"worker", `{}`, `{"snapshotAt":61000,"tasks":[{"status":"running","startedAt":1000,"result":{"status":"blocked","summary":["reason"]}}]}`, "", "! Task 1 · blocked"},
		{"codemode", `{"code":"await tools.read({})"}`, `{}`, "", "code::javascript:1"},
		{"ask_question", `{"question":"Which one?"}`, `{}`, "", "Question\nWhich one?"},
		{"future_tool", `{}`, `{"unknown":["retained"]}`, `{"answer":42}`, "Result\nanswer: 42"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := session.Item{ToolName: tt.name, Arguments: json.RawMessage(tt.args), Details: json.RawMessage(tt.details), Text: tt.text, HasResult: true}
			rows := Render(item, config.Theme{}, 0, testHooks(t), provider)
			text := strings.Join(rows, "\n")
			if !strings.Contains(text, tt.want) {
				t.Fatalf("missing %q in %s", tt.want, text)
			}
			if !reflect.DeepEqual(rows, Render(item, config.Theme{}, 0, testHooks(t), provider)) || string(item.Arguments) != tt.args || string(item.Details) != tt.details {
				t.Fatal("rendering mutated input or retained per-call state")
			}
			if tt.name == "future_tool" && !strings.Contains(text, "retained") {
				t.Fatal("unknown detail lost")
			}
		})
	}
}

func TestSharedFailureAndMetadataFallback(t *testing.T) {
	item := session.Item{ToolName: "write", Error: true, Arguments: json.RawMessage(`{"path":"a.go","content":"not written"}`), Text: "permission denied", Details: json.RawMessage(`["future metadata"]`)}
	text := strings.Join(Render(item, config.Theme{}, 60, testHooks(t)), "\n")
	if !strings.HasPrefix(text, "Tool failed\npermission denied") || strings.Contains(text, "not written") || !strings.Contains(text, "future metadata") {
		t.Fatal(text)
	}
	for _, raw := range []string{`{"a":`, `{} {}`, `not json`} {
		if _, ok := decodeJSON([]byte(raw)); ok {
			t.Fatalf("accepted invalid result JSON: %s", raw)
		}
	}
}
