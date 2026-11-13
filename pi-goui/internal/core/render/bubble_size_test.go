package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestShortBubbleFitsContentAndTitle(t *testing.T) {
	for _, tc := range []struct {
		role, text, title string
		width             int
	}{
		{"user", "你好", "你", 8}, {"user", "hi", "你", 8}, {"user", "👩‍💻", "你", 8},
		{"assistant", "你好", "助手", 10}, {"assistant", "**hi**", "助手", 10},
	} {
		for _, available := range []int{16, 60, 132} {
			d := Transcript([]session.Item{{ID: "short", Role: tc.role, Text: tc.text}}, testTheme(), Options{Width: available})
			rows := strings.Split(ansi.Strip(d.Text), "\n")
			if len(rows) != 3 {
				t.Fatalf("%s %q got %d rows: %q", tc.role, tc.text, len(rows), rows)
			}
			inset := 0
			if tc.role == "user" {
				inset = available - tc.width
			}
			for _, row := range rows {
				if !strings.HasPrefix(row, strings.Repeat(" ", inset)) {
					t.Fatal("alignment changed")
				}
				if got := ansi.StringWidth(strings.TrimPrefix(row, strings.Repeat(" ", inset))); got != tc.width {
					t.Fatalf("%s width=%d want=%d: %q", tc.role, got, tc.width, row)
				}
			}
			if !strings.Contains(rows[0], "╭─ "+tc.title+" ") {
				t.Fatalf("title clipped: %q", rows[0])
			}
			checkWidth(t, d.Text, available)
		}
	}
}

func TestBubbleMarkdownPaddingAndCodeIndentation(t *testing.T) {
	d := Transcript([]session.Item{{ID: "code", Role: "assistant", Text: "```text\n    alpha  beta\n      gamma\n```"}}, testTheme(), Options{Width: 132})
	plain := ansi.Strip(d.Text)
	if !strings.Contains(plain, "    alpha  beta") || !strings.Contains(plain, "      gamma") {
		t.Fatalf("code spacing lost: %q", plain)
	}
	for _, row := range strings.Split(plain, "\n") {
		if ansi.StringWidth(row) >= 60 {
			t.Fatalf("Markdown padding still dominates bubble: %q", row)
		}
	}
	colored := paint("  important  spacing   ", testTheme().Accent)
	rows, width := compactBubble([]string{colored}, "你", 80)
	if ansi.Strip(rows[0]) != "  important  spacing" || width != ansi.StringWidth("  important  spacing")+4 || !strings.Contains(rows[0], "\x1b[") {
		t.Fatalf("unsafe padding trim: %#v %d", rows, width)
	}
}

func TestSystemBubbleSynopsisAndBlockOffsets(t *testing.T) {
	original := []session.Item{{ID: "u", Role: "user", Text: "你好"}, {ID: "s", Role: "system", Text: "Context compacted — earlier messages retained\n# Summary\nKeep important context.\nHIDDEN_FULL_SUMMARY"}, {ID: "a", Role: "assistant", Text: "A short answer"}, {ID: "t", Role: "tool", ToolName: "bash", Text: "tool output"}}
	saved := append([]session.Item(nil), original...)
	d := Transcript(original, testTheme(), Options{Width: 80})
	rows := strings.Split(ansi.Strip(d.Text), "\n")
	system := blockByID(t, d, "s")
	if system.End-system.Start != 3 || !strings.HasPrefix(rows[system.Start], "╭─ 系统 ") || !strings.HasPrefix(rows[system.End-1], "╰") {
		t.Fatalf("wrong System frame: %+v %q", system, rows)
	}
	if !strings.Contains(rows[system.Start+1], "Context compacted · Keep important context.") || strings.Contains(d.Text, "HIDDEN_FULL_SUMMARY") {
		t.Fatal("summary was lost or expanded")
	}
	if !reflect.DeepEqual(original, saved) {
		t.Fatal("source history mutated")
	}
	previous := 0
	for _, b := range d.Blocks {
		if b.Start < previous || b.End > len(rows) || b.End <= b.Start {
			t.Fatalf("bad hit range: %+v", b)
		}
		previous = b.End
	}
	tool := blockByID(t, d, "tool-group:t")
	if !strings.Contains(rows[tool.Start], "bash") {
		t.Fatal("tool click range changed after compact framing")
	}
}

func TestCompactBubblesLongTextAndNarrowWidths(t *testing.T) {
	for _, width := range []int{1, 8, 16, 60, 132} {
		for _, role := range []string{"user", "assistant", "system"} {
			unicodeItem := session.Item{ID: "unicode", Role: role, Text: "你好 é 👩‍💻"}
			d := Transcript([]session.Item{unicodeItem}, testTheme(), Options{Width: width})
			checkWidth(t, d.Text, width)
			if width >= 16 && !strings.Contains(ansi.Strip(d.Text), "你好 é 👩‍💻") {
				t.Fatalf("Unicode content split or lost: %q", d.Text)
			}
		}
		for _, role := range []string{"user", "assistant", "system"} {
			text := "ASCII words with internal  spaces and a final marker END"
			if role != "system" {
				text = strings.Repeat("long words ", 30) + "END"
			}
			d := Transcript([]session.Item{{ID: "x", Role: role, Text: text}}, testTheme(), Options{Width: width})
			checkWidth(t, d.Text, width)
			if role != "system" && width >= 8 && !strings.Contains(ansi.Strip(d.Text), "END") {
				t.Fatalf("long message truncated: width=%d role=%s", width, role)
			}
		}
	}
}
