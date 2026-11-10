package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func codemodeFixture() session.Item {
	return session.Item{ID: "cm1", Role: "tool", ToolName: "codemode", HasResult: true, EstimatedTokens: 5400,
		Arguments: json.RawMessage(`{"code":"const r = await tools.read({path: \"src/main.go\"});\ntext(r);"}`),
		Details:   json.RawMessage(`{"calls":[{"id":0,"name":"read","args":"{\"path\":\"src/main.go\",\"offset\":1}","status":"ok","durationMs":409},{"id":1,"name":"bash","args":"{\"command\":\"pwd\"}","status":"running"}],"execution":{"internal":"not a display dump"}}`),
		Text:      "Script completed\nWall time 0.5 seconds\nOutput:\n\nUSER_RETURNED_TEXT"}
}
func TestCodemodeScreenshotTraceAndTitle(t *testing.T) {
	item := codemodeFixture()
	theme := testTheme()
	rows := toolBody(item, theme, Options{ExpandTools: true}, 100)
	raw := strings.Join(rows, "\n")
	plain := ansi.Strip(raw)
	for _, want := range []string{"┌─ codemode ≈5.4k tok", "const r = await tools.read", "text(r);", "✓ read", "path: src/main.go", "offset: 1", "409ms", "⋯ bash", "USER_RETURNED_TEXT"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q:\n%s", want, plain)
		}
	}
	for _, bad := range []string{"JavaScript", "Metadata", "\nResult\n", "Script completed", "1 │", "\"calls\":", "not a display dump", "codemode · result"} {
		if strings.Contains(plain, bad) {
			t.Fatalf("generic/protocol presentation leaked %q", bad)
		}
	}
	if !strings.Contains(rows[1], tokenBadge(5400, theme)) {
		t.Fatal("token title lost deliberate badge color")
	}
	if !strings.Contains(raw, paint("read", theme.Accent)) || !strings.Contains(raw, paint("✓", theme.Success)) {
		t.Fatal("call name/status colors missing")
	}
	if len(toolBody(item, theme, Options{}, 100)) != 1 {
		t.Fatal("collapsed invocation changed")
	}
	ordinary := item
	ordinary.ToolName = "read"
	ordinary.Text = "hello"
	if !strings.Contains(ansi.Strip(strings.Join(toolBody(ordinary, theme, Options{ExpandTools: true}, 100), "\n")), "read · result") {
		t.Fatal("non-codemode frame changed")
	}
}
func TestCodemodeErrorsPartialAndReturnedStructures(t *testing.T) {
	item := codemodeFixture()
	item.Running = true
	item.Error = true
	item.Details = json.RawMessage(`{"calls":[{"name":"edit","args":"{\"path\":\"x.go\"}","status":"error","durationMs":1200,"error":"missing old text\nsecond line"},{"name":"model","args":"small","status":"cancelled","cost":0.00012}],"fullOutputPath":"/tmp/saved-output.txt"}`)
	item.Text = "Script failed\nWall time 1.2 seconds\nOutput:\n{\"records\":[{\"name\":\"kept-value\"}]}"
	plain := ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
	for _, want := range []string{"✗ edit", "1.2s", "missing old text", "second line", "⊘ model", "$0.00012", "kept-value", "Script failed", "/tmp/saved-output.txt"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %s", want, plain)
		}
	}
	if strings.Contains(plain, `{"records":`) {
		t.Fatal("returned JSON was dumped instead of structured")
	}
	item.Details = nil
	item.Text = "partial explicit output"
	item.Error = false
	plain = ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
	if !strings.Contains(plain, "partial explicit output") || !strings.Contains(plain, "Running…") {
		t.Fatal("partial output hidden")
	}
	item.Arguments = json.RawMessage(`{"code":"await tools.read({path:'not-a-trace'});"}`)
	item.Running = false
	item.Text = ""
	plain = ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
	if strings.Contains(plain, "✓ read") {
		t.Fatal("invented internal call from source")
	}
	item.Details = json.RawMessage(`{"calls":[{"name":"read","args":"{\"path\":...","status":"running"},null]}`)
	plain = ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
	if !strings.Contains(plain, "incomplete preview") || !strings.Contains(plain, "entry unavailable") {
		t.Fatal("malformed trace not explained")
	}
}
func TestCodemodeWidthsAndTerminalSafety(t *testing.T) {
	item := codemodeFixture()
	source := "const 界 = '👩‍💻';\n\ttext(界);\x1b]52;c;ATTACK\x07"
	item.Arguments, _ = json.Marshal(map[string]string{"code": source})
	item.Details, _ = json.Marshal(map[string]any{"calls": []any{map[string]any{"name": "read\x1b[2J", "args": "{\"path\":\"中文.go\"}", "status": "error", "durationMs": 409, "error": "bad\x1b]0;ATTACK\x07"}}})
	for width := 1; width <= 132; width++ {
		body := strings.Join(toolBody(item, testTheme(), Options{ExpandTools: true}, width), "\n")
		checkWidth(t, body, width)
		if strings.Contains(body, "ATTACK") || strings.Contains(body, "\x1b]") || strings.Contains(body, "\x1b[2J") {
			t.Fatalf("hostile terminal command leaked at width %d", width)
		}
		if strings.Contains(body, "\x1b[48;") {
			t.Fatal("background fill")
		}
	}
}
