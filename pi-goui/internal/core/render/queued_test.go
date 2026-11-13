package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
)

func TestQueuedBubble(t *testing.T) {
	for _, width := range []int{1, 8, 20, 80} {
		rows := Queued([]string{"first message", "second message"}, "alt+up", width, 9, config.Theme{})
		plain := ansi.Strip(strings.Join(rows, "\n"))
		if len(rows) > 9 {
			t.Fatalf("queue exceeded height: %d", len(rows))
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > width {
				t.Fatalf("width %d overflow: %q", width, row)
			}
		}
		if width == 80 {
			for _, want := range []string{"你 queued", "first message", "second message", "alt+up"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("missing %q: %s", want, plain)
				}
			}
			if strings.Count(plain, "你 queued") != 1 {
				t.Fatal("messages should share a bubble")
			}
		}
	}
	if rows := Queued(nil, "alt+up", 80, 9, config.Theme{}); len(rows) != 0 {
		t.Fatal("empty queue should be hidden")
	}
	rows := Queued([]string{strings.Repeat("line\n", 100), "last"}, "alt+up", 80, 6, config.Theme{})
	if len(rows) > 6 || !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "…") {
		t.Fatal("long queues should be compressed")
	}
}
