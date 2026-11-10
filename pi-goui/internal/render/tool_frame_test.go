package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestExpandedToolHasSeparateTransparentFrame(t *testing.T) {
	item := session.Item{ID: "tool:read-1", Role: "tool", ToolName: "read", Arguments: json.RawMessage(`{"path":"src/main.go"}`), Text: "FIRST_LINE\n第二行 é 👩‍💻\nLAST_LINE"}
	for _, width := range []int{20, 60, 80, 132} {
		rows := toolBody(item, testTheme(), Options{Expanded: map[string]bool{item.ID: true}}, width)
		plain := strings.Split(ansi.Strip(strings.Join(rows, "\n")), "\n")
		if !strings.HasPrefix(plain[0], "⌄ ✓ read") || strings.ContainsAny(plain[0], "╭╮│") {
			t.Fatalf("invocation must remain outside result frame: %q", plain[0])
		}
		if !strings.HasPrefix(plain[1], "╭─ read") || !strings.HasSuffix(plain[1], "╮") || !strings.HasPrefix(plain[len(plain)-1], "╰") {
			t.Fatalf("missing result frame: %q", plain)
		}
		for _, row := range plain[2 : len(plain)-1] {
			if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
				t.Fatalf("unframed result row: %q", row)
			}
		}
		all := strings.Join(rows, "\n")
		checkWidth(t, all, width)
		if !strings.Contains(all, "FIRST_LINE") || !strings.Contains(all, "LAST_LINE") {
			t.Fatal("framing lost output")
		}
	}
	collapsed := toolBody(item, testTheme(), Options{}, 60)
	if len(collapsed) != 1 || strings.ContainsAny(ansi.Strip(collapsed[0]), "╭╮│") {
		t.Fatal("collapsed tool should remain a single invocation row")
	}
}
func TestGroupedExpandedToolFrameHitRange(t *testing.T) {
	items := toolFixture()
	opts := Options{Width: 60, Expanded: map[string]bool{"tool-group:t1": true, "t2": true}}
	doc := Transcript(items, testTheme(), opts)
	rows := strings.Split(ansi.Strip(doc.Text), "\n")
	child := blockByID(t, doc, "t2")
	group := blockByID(t, doc, "tool-group:t1")
	if group.End > child.Start || !child.Expanded {
		t.Fatal("group/child hit areas overlap")
	}
	body := strings.Join(rows[child.Start:child.End], "\n")
	if !strings.Contains(body, "╭─ read · result") || !strings.Contains(body, "SECOND_OUTPUT") || !strings.Contains(rows[child.End-1], "╰") {
		t.Fatalf("child hit area must include complete result frame: %q", body)
	}
	checkWidth(t, doc.Text, 60)
	for _, width := range []int{1, 4, 8, 12, 16} {
		checkWidth(t, Transcript(items, testTheme(), Options{Width: width, ExpandTools: true}).Text, width)
	}
}
