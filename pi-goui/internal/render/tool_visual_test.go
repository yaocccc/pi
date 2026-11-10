package render

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func visualItem(name string, args, details any, text string) session.Item {
	a, _ := json.Marshal(args)
	d, _ := json.Marshal(details)
	return session.Item{Role: "tool", ToolName: name, Arguments: a, Details: d, Text: text, HasResult: true}
}
func visualText(item session.Item, width int) string {
	return strings.Join(toolDetailRows(item, testTheme(), width), "\n")
}
func mustContainVisual(t *testing.T, text string, parts ...string) {
	t.Helper()
	text = ansi.Strip(text)
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in:\n%s", part, text)
		}
	}
}
func TestSemanticReadNumberingAndSavedCode(t *testing.T) {
	item := visualItem("read", map[string]any{"path": "/does/not/exist/source.go", "offset": 12, "limit": 3}, nil, "package demo\n\tfunc main() { println(42) }\n// saved")
	before := item.Text
	rendered := visualText(item, 80)
	mustContainVisual(t, rendered, "/does/not/exist/source.go", "lines 12–14", "12 │ package demo", "13 │     func main()", "14 │ // saved")
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatal("code was not styled")
	}
	if item.Text != before {
		t.Fatal("source changed")
	}
	if strings.Contains(ansi.Strip(rendered), `"offset":`) {
		t.Fatal("protocol JSON leaked")
	}
	source := "alpha\nbeta"
	item.Text = source + "\n\n[Showing lines 12-13 of 400. Use offset=14 to continue.]"
	item.Details = json.RawMessage(`{"truncation":{"content":"alpha\nbeta","truncated":true,"outputLines":2,"totalLines":400}}`)
	rendered = visualText(item, 100)
	mustContainVisual(t, rendered, "12 │ alpha", "13 │ beta", "Read notice", "Use offset=14", "Truncated: yes", "Total lines: 400")
	if strings.Count(ansi.Strip(rendered), "alpha") != 1 {
		t.Fatal("truncation metadata duplicated source")
	}
	item.Text = "Read image file [image/png]\n[image]"
	item.Details = nil
	mustContainVisual(t, visualText(item, 80), "Image attachment", "image/png")
	item.Error = true
	item.Text = "Permission denied"
	rendered = visualText(item, 80)
	mustContainVisual(t, rendered, "Error", "Permission denied")
	if strings.Contains(ansi.Strip(rendered), "Read preview") {
		t.Fatal("error rendered as file contents")
	}
}
func TestSemanticEditNativeAndUnifiedDiff(t *testing.T) {
	for _, diff := range []string{" 10 before\n-11 old()\n+11 new()\n 12 after", "--- a/a.go\n+++ b/a.go\n@@ -10,3 +10,3 @@\n before\n-old()\n+new()\n after"} {
		item := visualItem("edit", map[string]any{"path": "a.go", "oldText": "wrong guessed old", "newText": "wrong guessed new"}, map[string]any{"diff": diff, "firstChangedLine": 11}, "Successfully replaced text")
		text := visualText(item, 100)
		mustContainVisual(t, text, "Reported diff", "+1 added", "−1 removed", "old()", "new()", "First changed line: 11")
		if strings.Contains(text, "wrong guessed") {
			t.Fatal("ignored authoritative diff")
		}
		if !strings.Contains(text, paint(strings.Split(diff, "\n")[len(strings.Split(diff, "\n"))-2], testTheme().Success)) && !strings.Contains(text, "\x1b[") {
			t.Fatal("missing diff styles")
		}
		item.Error = true
		text = visualText(item, 100)
		mustContainVisual(t, text, "completion not confirmed", "Error")
	}
	if added, removed := diffCounts("--- a/test\n+++ b/test\n@@ -1 +1 @@\n--- removed code\n+++ added code"); added != 1 || removed != 1 {
		t.Fatalf("hunk text confused with file headers: +%d -%d", added, removed)
	}
	patch := visualItem("edit", map[string]any{"path": "a.go"}, map[string]any{"patch": "@@ -1 +1 @@\n-old\n+new"}, "changed")
	mustContainVisual(t, visualText(patch, 80), "+1 added", "−1 removed")
}
func TestSemanticMultipleReplacementFallback(t *testing.T) {
	item := visualItem("edit", map[string]any{"path": "a.ts", "edits": []any{map[string]any{"oldText": "const a = 1;", "newText": "const a = 2;"}, map[string]any{"oldText": "old\nlast", "newText": "new\nlast"}}}, nil, "Done")
	text := visualText(item, 120)
	mustContainVisual(t, text, "Requested replacement 1", "Requested replacement 2", "relative lines, not a recorded file diff", "− Before", "+ After", "const a = 1;", "const a = 2;")
	item.Error = true
	item.Text = "No unique match"
	text = visualText(item, 120)
	mustContainVisual(t, text, "Requested replacement", "No unique match")
	if strings.Contains(text, "Applied") {
		t.Fatal("failed operation presented as applied")
	}
	legacy := visualItem("edit", map[string]any{"path": "a.py", "oldText": "old", "newText": "new"}, nil, "done")
	mustContainVisual(t, visualText(legacy, 120), "Requested replacement 1", "Before", "After")
}
func TestSemanticWriteOutcomeAndContent(t *testing.T) {
	item := visualItem("write", map[string]any{"path": "file.ts", "content": "const n = 42;\nconsole.log(n);"}, nil, "Successfully wrote to file.ts")
	mustContainVisual(t, visualText(item, 80), "Written content · 2 lines", "1 │ const n = 42;", "2 │ console.log(n);")
	item.Error = true
	mustContainVisual(t, visualText(item, 80), "Write failed · requested content")
	item.Error = false
	item.HasResult = false
	mustContainVisual(t, visualText(item, 80), "Requested content · completion not confirmed")
	item.Running = true
	mustContainVisual(t, visualText(item, 80), "Writing · requested content", "Partial result")
}
func TestSemanticCommonAndUnknownTools(t *testing.T) {
	fixtures := []struct {
		item   session.Item
		wanted []string
	}{
		{visualItem("bash", map[string]any{"command": "printf hello", "timeout": 10}, map[string]any{"exitCode": 0}, "hello"), []string{"Command", "printf hello", "Timeout: 10", "Exit code: 0", "hello"}},
		{visualItem("grep", map[string]any{"pattern": "needle", "path": "src"}, nil, "src/a.go:4: needle"), []string{"File  src", "Match  needle", "Matches", "src/a.go:4: needle"}},
		{visualItem("find", map[string]any{"pattern": "*.go", "path": "src"}, nil, "a.go\nb.go"), []string{"Matches", "a.go", "b.go"}},
		{visualItem("ls", map[string]any{"path": "src"}, nil, "a.go\nsubdir/"), []string{"Matches", "subdir/"}},
		{visualItem("codemode", map[string]any{"code": "const result = await tools.read({path: 'a.go'});"}, nil, "done"), []string{"const result", "done"}},
		{visualItem("worker", map[string]any{"tasks": []any{map[string]any{"objective": "Run unit tests", "mode": "test"}}}, nil, `{"status":"completed","results":[{"task":"Unit tests","status":"passed"}]}`), []string{"✓ test", "Run unit tests"}},
		{visualItem("ask_question", map[string]any{"question": "Pick one", "options": []string{"Alpha", "Beta"}}, nil, `{"value":"Beta"}`), []string{"Questions and choices", "Question: Pick one", "1: Alpha", "2: Beta", "Value: Beta"}},
		{visualItem("custom_extension", map[string]any{"enabled": true, "rules": []any{map[string]any{"scope": "src", "checks": []string{"lint", "test"}}}, "nullable": nil}, map[string]any{"durationMs": 12}, `{"matches":[{"path":"src/main.go","line":3}],"message":"Found one"}`), []string{"Enabled: yes", "Nullable: —", "Scope: src", "1: lint", "2: test", "Path: src/main.go", "Line: 3", "Message: Found one", "Duration ms: 12"}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.item.ToolName, func(t *testing.T) {
			text := visualText(fixture.item, 120)
			mustContainVisual(t, text, fixture.wanted...)
			plain := ansi.Strip(text)
			if fixture.item.ToolName != "codemode" && strings.ContainsAny(plain, "{}[]") {
				t.Fatalf("protocol object dumped: %s", plain)
			}
		})
	}
}
func TestSemanticGenericRetainsNestedFieldsAndText(t *testing.T) {
	item := visualItem("plugin", map[string]any{"nested": map[string]any{"a": []any{1, false, nil}, "multiline": "line one\nline two"}}, nil, `{"output":"{\"answer\":42}","items":[]}`)
	text := visualText(item, 80)
	mustContainVisual(t, text, "Nested", "1: 1", "2: no", "3: —", "line one", "line two", "Answer: 42", "empty list")
	item.Arguments = json.RawMessage(`{"still`)
	item.Running = true
	item.Text = ""
	mustContainVisual(t, visualText(item, 80), "Parameters unavailable", "Waiting for output")
	item.Arguments = json.RawMessage("null")
	item.Running = false
	item.HasResult = true
	mustContainVisual(t, visualText(item, 80), "empty result")
	image := visualItem("plugin", nil, nil, `{"mimeType":"image/png","data":"SECRET_BASE64","width":100}`)
	text = visualText(image, 80)
	mustContainVisual(t, text, "Image attachment", "Width: 100")
	if strings.Contains(text, "SECRET_BASE64") {
		t.Fatal("binary dumped as text")
	}
	// JSON documents are legitimate file contents, not protocol wrappers.
	doc := visualItem("read", map[string]any{"path": "settings.json"}, nil, `{"enabled":true}`)
	mustContainVisual(t, visualText(doc, 80), `{"enabled":true}`)
}
func TestSemanticPreviewNeverDumpsObjectJSON(t *testing.T) {
	fixtures := []session.Item{visualItem("read", map[string]any{"path": "a.go", "offset": 14}, nil, "package main"), visualItem("edit", map[string]any{"path": "a.go"}, map[string]any{"diff": "-1 old\n+1 new"}, "done"), visualItem("write", map[string]any{"path": "a.go", "content": "a\nb"}, nil, "done"), visualItem("plugin", nil, nil, `{"result":{"items":[1,2]}}`), visualItem("plugin", nil, nil, `[{"a":1}]`)}
	for _, item := range fixtures {
		p := toolPreview(item)
		if strings.ContainsAny(p, "{}[]\n\x1b") {
			t.Fatalf("raw protocol preview: %q", p)
		}
	}
	if p := toolPreview(fixtures[1]); !strings.Contains(p, "+1 / −1") {
		t.Fatal(p)
	}
	if p := toolPreview(fixtures[2]); !strings.Contains(p, "2 lines") {
		t.Fatal(p)
	}
}
func TestSemanticWidthsControlSafetyAndTransparency(t *testing.T) {
	hostile := "\x1b[41mred\x1b[0m\x1b]52;c;ZXZpbA==\a\x1b[2J\r\t中文 é 👩‍💻\n" + strings.Repeat("wide", 70)
	fixtures := []session.Item{visualItem("read", map[string]any{"path": "file.go"}, nil, hostile), visualItem("write", map[string]any{"path": "a.ts", "content": hostile}, nil, "done"), visualItem("edit", map[string]any{"path": "a.go"}, map[string]any{"diff": "+1 " + hostile}, "done"), visualItem("plugin", map[string]any{hostile: []string{hostile}}, map[string]any{"extra": hostile}, hostile), visualItem("codemode", map[string]any{"code": hostile}, nil, hostile)}
	sgr := regexp.MustCompile("\x1b\\[([0-9;:]*)m")
	for _, width := range []int{1, 2, 4, 7, 12, 20, 60, 80, 132} {
		for _, item := range fixtures {
			t.Run(fmt.Sprintf("%s-%d", item.ToolName, width), func(t *testing.T) {
				text := visualText(item, width)
				checkWidth(t, text, width)
				if strings.Contains(text, "\x1b]") || strings.Contains(text, "\x1b[2J") || strings.Contains(text, "ZXZpbA==") {
					t.Fatal("terminal command survived")
				}
				for _, m := range sgr.FindAllStringSubmatch(text, -1) {
					parts := strings.FieldsFunc(m[1], func(r rune) bool { return r == ';' || r == ':' })
					for i := 0; i < len(parts); i++ {
						n := parts[i]
						if n == "38" {
							if i+1 < len(parts) && parts[i+1] == "2" {
								i += 4
							} else {
								i += 2
							}
							continue
						}
						if n == "48" || n == "40" || n == "41" || n == "42" || n == "43" || n == "44" || n == "45" || n == "46" || n == "47" || n == "100" || n == "101" || n == "102" || n == "103" || n == "104" || n == "105" || n == "106" || n == "107" {
							t.Fatalf("background SGR: %s", m[0])
						}
					}
				}
			})
		}
	}
}
func TestSemanticConcurrentHighlightAndCompleteLargeOutput(t *testing.T) {
	source := strings.Repeat("const retained = '中文';\n", 12000) + "FINAL_CONTENT_MARKER"
	item := visualItem("write", map[string]any{"path": "large.ts", "content": source}, nil, "done")
	text := ansi.Strip(visualText(item, 80))
	if strings.Count(text, "const retained") != 12000 || !strings.Contains(text, "FINAL_CONTENT_MARKER") {
		t.Fatal("large saved output was truncated")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 8; n++ {
				rendered := visualText(visualItem("read", map[string]any{"path": "a.go"}, nil, "package main\nfunc main() {}"), 50)
				if !strings.Contains(ansi.Strip(rendered), "package main") {
					t.Error("lost code")
				}
			}
		}()
	}
	wg.Wait()
}
