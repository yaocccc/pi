package tooldetail

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

func TestGenericStructuredResultsAndDetails(t *testing.T) {
	for _, tc := range []struct{ input, details, want string }{
		{`42`, `{"extra":"retained"}`, "Result\n42\nDetails\nextra: retained"},
		{`"hello"`, `{}`, "Result\nhello"},
		{`[1,"two",{"nested":"three"}]`, `{}`, "• 1\n• two\n#3\n  nested: three"},
		{`{"x":{"y":"value"}}`, `{}`, "x\n  y: value"},
	} {
		item := session.Item{ToolName: "plugin", Text: tc.input, Details: json.RawMessage(tc.details)}
		got := strings.Join(Render(item, config.Theme{}, 80, testHooks(t)), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q; want %q", tc.input, got, tc.want)
		}
	}
	item := session.Item{ToolName: "plugin", Text: `{"answer":42,"nested":{"x":"keep"}}`, Details: json.RawMessage(`{"answer":42,"nested":{"x":"keep"}}`)}
	got := strings.Join(Render(item, config.Theme{}, 80, testHooks(t)), "\n")
	if strings.Count(got, "answer: 42") != 1 || strings.Contains(got, "Details") || !strings.Contains(got, "x: keep") {
		t.Fatal(got)
	}
	item.Details = json.RawMessage(`{"answer":42,"extra":"keep"}`)
	got = strings.Join(Render(item, config.Theme{}, 80, testHooks(t)), "\n")
	if !strings.Contains(got, "extra: keep") || !strings.Contains(got, "x: keep") {
		t.Fatal(got)
	}
}

func TestGenericHierarchyAndPrecision(t *testing.T) {
	item := session.Item{ToolName: "future_tool", Running: true,
		Arguments: json.RawMessage(`{"query":"中文查询","limit":900719925474099312345,"options":[true,null]}`),
		Text:      `{"count":900719925474099312345,"ratio":1.234567890123456789e-300,"ok":false,"empty":null,"body":"first\n\n  last \n","records":[{"name":"中文😀","unknown":[true,null,{}]}]}`,
		Details:   json.RawMessage(`{"extra":"retained"}`),
	}
	originalArgs, originalDetails := string(item.Arguments), string(item.Details)
	theme := config.Theme{Muted: "muted", Accent: "accent", Text: "text", Warning: "warning"}
	hooks := testHooks(t)
	colors := map[string]string{}
	hooks.Line = func(text, color string, width int) []string {
		colors[text] = color
		return strings.Split(text, "\n")
	}
	got := strings.Join(Render(item, theme, 40, hooks), "\n")
	for _, want := range []string{"Parameters", "query: 中文查询", "limit: 900719925474099312345", "● Running", "Object · 6 fields\nResult", "count: 900719925474099312345", "ratio: 1.234567890123456789e-300", "ok: false", "empty: null", "body\nfirst\n\n  last \n", "name: 中文😀", "• true", "• null", "{}", "extra: retained"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if colors["query: 中文查询"] != theme.Muted || colors["count: 900719925474099312345"] != theme.Accent || colors["● Running"] != theme.Warning {
		t.Fatalf("parameter/status/scalar colors: %#v", colors)
	}
	if string(item.Arguments) != originalArgs || string(item.Details) != originalDetails {
		t.Fatal("mutated inputs")
	}
}

func TestGenericEmptyAndPendingStates(t *testing.T) {
	for _, tc := range []struct {
		item session.Item
		want string
	}{
		{session.Item{ToolName: "unknown"}, "No result recorded"},
		{session.Item{ToolName: "unknown", Running: true}, "● Running\nWaiting for output…"},
		{session.Item{ToolName: "unknown", HasResult: true}, "(empty result)"},
		{session.Item{ToolName: "unknown", HasResult: true, Details: json.RawMessage(`{ }`)}, "(empty result)"},
		{session.Item{ToolName: "unknown", HasResult: true, Details: json.RawMessage(`null`)}, "(empty result)"},
		{session.Item{ToolName: "unknown", Text: "null"}, "Result\nnull"},
		{session.Item{ToolName: "unknown", Text: "false"}, "Result\nfalse"},
		{session.Item{ToolName: "unknown", Text: "{}"}, "Object · 0 fields\nResult\n{}"},
		{session.Item{ToolName: "unknown", Text: "[]"}, "List · 0 items\nResult\n[]"},
	} {
		got := strings.Join(Render(tc.item, config.Theme{}, 40, testHooks(t)), "\n")
		if got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	item := session.Item{ToolName: "unknown", Arguments: json.RawMessage(`{"partial":"value`), Text: "stream"}
	got := strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	if !strings.Contains(got, "Parameters unavailable") || !strings.Contains(got, string(item.Arguments)) || !strings.HasSuffix(got, "Output\nstream") {
		t.Fatal(got)
	}
	item.Error = true
	got = strings.Join(Render(item, config.Theme{}, 40, testHooks(t)), "\n")
	if !strings.HasPrefix(got, "Tool failed\nstream") || strings.Contains(got, "Parameters") {
		t.Fatal("error priority lost: " + got)
	}
}

func TestGenericWrapsWithoutTruncating(t *testing.T) {
	item := session.Item{ToolName: "plugin", Text: `{"nested":[{"long":"` + strings.Repeat("中文😀", 40) + `END","value":98765432109876543210}],"body":"start\n\n  finish "}`}
	for _, width := range []int{1, 2, 5, 20, 40, 60} {
		hooks := testHooks(t)
		hooks.Line = func(text, color string, gotWidth int) []string {
			if gotWidth != width {
				t.Fatalf("width %d, expected %d", gotWidth, width)
			}
			// The parent owns handling two-cell glyphs in a one-cell viewport;
			// here verify this renderer delegates without ever truncating first.
			return strings.Split(ansi.Hardwrap(text, gotWidth, true), "\n")
		}
		rows := Render(item, config.Theme{}, width, hooks)
		joined := strings.Join(rows, "")
		for _, want := range []string{strings.Repeat("中文😀", 40) + "END", "98765432109876543210", "  finish "} {
			if !strings.Contains(joined, want) {
				t.Errorf("width %d lost %q", width, want)
			}
		}
		if width >= 2 {
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Errorf("row exceeds width %d: %q", width, row)
				}
			}
		}
	}
}

func TestGenericPreservesTextAndUsesCleanHook(t *testing.T) {
	for _, input := range []string{"first\n\n  last \n", " \n\n", "{broken\n\ntext"} {
		got := strings.Join(Render(session.Item{ToolName: "plugin", Text: input}, config.Theme{}, 80, testHooks(t)), "\n")
		if got != "Output\n"+input {
			t.Errorf("got %q, want original %q", got, input)
		}
	}
	hooks := testHooks(t)
	hooks.Clean = func(s string) string { return strings.ReplaceAll(s, "\x1b", "") }
	got := strings.Join(Render(session.Item{ToolName: "plugin", Text: `{"value":"\u001b[31mtext"}`}, config.Theme{}, 80, hooks), "\n")
	if strings.Contains(got, "\x1b") || !strings.Contains(got, "[31mtext") {
		t.Fatal(got)
	}
}
