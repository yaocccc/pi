package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func TestEditDetailRecordedAndPreviewHierarchy(t *testing.T) {
	item := session.Item{ToolName: "edit", HasResult: true, Arguments: json.RawMessage(`{"path":"file.go","edits":[{"oldText":"before\n\n","newText":"after\n"},{"oldText":"","newText":"插入🙂"}]}`)}
	text := strings.Join(Render(item, config.Theme{}, 25, testHooks(t)), "\n")
	for _, want := range []string{"File · file.go", "Replacement preview · not a recorded diff", "Replacement 1", "− old · 2 source lines", "+ new · 1 source lines", "Replacement 2", "− old · 0 source lines", "插入🙂"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	item.Details = json.RawMessage(`{"diff":"-10 before\n+10 after","firstChangedLine":10,"future":{"note":"KEEP"}}`)
	text = strings.Join(Render(item, config.Theme{}, 25, testHooks(t)), "\n")
	for _, want := range []string{"✓ Changes recorded", "Diff · +1 −1", "firstChangedLine: 10", "note: KEEP"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "Replacement preview") || strings.Count(text, "-10 before") != 1 {
		t.Fatal(text)
	}
	item.Running = true
	text = strings.Join(Render(item, config.Theme{}, 25, testHooks(t)), "\n")
	if !strings.Contains(text, "completion not confirmed") || strings.Contains(text, "Changes recorded") {
		t.Fatal(text)
	}
}

func TestEditDetailUnifiedRailsAndHunkColors(t *testing.T) {
	diff := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n--- removed source\n+++ 中文🙂 added source\n unchanged\n\n\\ No newline at end of file"
	for _, width := range []int{10, 20, 60} {
		hooks := testHooks(t)
		colors := map[string]string{}
		hooks.Line = func(text, color string, w int) []string {
			colors[text] = color
			return strings.Split(ansi.Hardwrap(text, w, true), "\n")
		}
		theme := config.Theme{Text: "body", Muted: "muted", Accent: "heading", Success: "added", Error: "removed"}
		v := renderer{theme: theme, width: width, hooks: hooks}
		v.editDiff(diff)
		if colors["--- a/x"] != theme.Accent || colors["+++ b/x"] != theme.Accent || colors["--- removed source"] != theme.Error || colors["+++ 中文🙂 added source"] != theme.Success {
			t.Fatalf("hunk/file lines misclassified: %#v", colors)
		}
		var content strings.Builder
		for _, row := range v.rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("width=%d overflow=%q", width, row)
			}
			if !strings.HasPrefix(row, "▌ ") && !strings.HasPrefix(row, "│ ") {
				t.Fatalf("missing continuation rail: %q", row)
			}
			content.WriteString(ansi.Cut(row, 2, ansi.StringWidth(row)))
		}
		if content.String() != strings.ReplaceAll(diff, "\n", "") {
			t.Fatalf("source changed during wrapping: %q", content.String())
		}
		if !strings.Contains(strings.Join(v.rows, "\n"), "\n│ \n") {
			t.Fatal("real empty line disappeared")
		}
	}
}

func TestEditDetailPreviewRailsPreserveFullSource(t *testing.T) {
	oldSource, newSource := "old\n\n"+strings.Repeat("old tail\n", 30), "中文🙂 new\n"
	args, _ := json.Marshal(map[string]any{"path": "source.go", "oldText": oldSource, "newText": newSource})
	hooks := testHooks(t)
	var sources []string
	hooks.CodeRows = func(source, path, language string, start, width int, theme config.Theme) []string {
		if path != "source.go" || start != 1 || width != 18 {
			t.Fatalf("incorrect preview coordinates: %s %d %d", path, start, width)
		}
		sources = append(sources, source)
		return []string{"line", "wrapped continuation"}
	}
	text := strings.Join(Render(session.Item{ToolName: "edit", Arguments: args}, config.Theme{}, 20, hooks), "\n")
	if len(sources) != 2 || sources[0] != oldSource || sources[1] != newSource || strings.Count(text, "▌ ") != 4 || !strings.Contains(text, "relative to each replacement") {
		t.Fatal("preview lost source or rail", text)
	}
}

func TestEditDetailMissingChanges(t *testing.T) {
	text := strings.Join(Render(session.Item{ToolName: "edit"}, config.Theme{}, 30, testHooks(t)), "\n")
	if !strings.Contains(text, "waiting for changes") {
		t.Fatal(text)
	}
}
