package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestTranscriptMessageTargets(t *testing.T) {
	const site = "https://example.org/guide?q=42"
	const path = "internal/core/render/render.go:12:3"
	items := []session.Item{
		{ID: "u", Role: "user", Text: "前面🙂 [文档](" + site + ") and [file](my%20notes/README.md)"},
		{ID: "t", Role: "tool", ToolName: "read", Text: path},
		{ID: "a", Role: "assistant", Thinking: "not actionable: /hidden.go", Text: "Open `" + path + "` or ~/其它/文件.txt。"},
		{ID: "s", Role: "system", Text: "https://example.org/ignored"},
	}
	for _, width := range []int{17, 38, 90} {
		opts := Options{Width: width, ShowThinking: true, ExpandTools: true}
		d := Transcript(items, testTheme(), opts)
		var c TranscriptCache
		cached := c.Transcript(items, testTheme(), opts)
		if !reflect.DeepEqual(cached.messageTargets, d.messageTargets) {
			t.Fatalf("cached offsets differ at %d", width)
		}
		if strings.Contains(d.FullText(), "\x1b]8;") {
			t.Fatal("transcript delegated opening to terminal")
		}
		rows := strings.Split(d.FullText(), "\n")
		displayed := map[string]string{}
		for _, hit := range d.messageTargets {
			line := ansi.Strip(rows[hit.row])
			displayed[hit.target] += ansi.Cut(line, hit.start, hit.end)
			span, _ := d.Span(hit.row)
			if !span.Content || hit.start < span.Start || hit.end > span.End {
				t.Fatalf("%+v outside body %+v", hit, span)
			}
			for x := hit.start; x < hit.end; x++ {
				if got, ok := d.MessageTargetAt(hit.row, x); !ok || got != hit.target {
					t.Fatalf("bad hit at %+v cell %d: %q", hit, x, got)
				}
			}
		}
		for target, want := range map[string]string{site: "文档" + site, "my notes/README.md": "file/my%20notes/README.md", path: path, "~/其它/文件.txt": "~/其它/文件.txt"} {
			if got := displayed[target]; got != want {
				t.Errorf("width %d target %q: displayed %q, want %q", width, target, got, want)
			}
		}
		if len(displayed) != 4 {
			t.Errorf("unexpected hits: %+v", displayed)
		}
		if _, ok := d.MessageTargetAt(0, 0); ok {
			t.Fatal("frame accepts hits")
		}
	}
}

func TestMessageTargetDisallowsTerminalCommands(t *testing.T) {
	d := Transcript([]session.Item{{ID: "u", Role: "user", Text: "[no](javascript:alert) or \x1b]8;;https://example.org\aordinary label\x1b]8;;\a"}}, testTheme(), Options{Width: 80})
	if len(d.messageTargets) != 0 || strings.Contains(d.FullText(), "\x1b]8;") {
		t.Fatal("unsafe or source-provided OSC link survived")
	}
}
