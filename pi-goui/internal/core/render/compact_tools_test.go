package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestReadAndGenericToolBoundedPreviews(t *testing.T) {
	for _, name := range []string{"read", "grep", "find", "ls", "mcp.example", "custom"} {
		t.Run(name, func(t *testing.T) {
			output := strings.Repeat("retained line\n", 20) + "HIDDEN_TAIL"
			item := visualItem(name, map[string]any{"path": "example.go", "offset": 7}, map[string]any{"debug": "HIDDEN_METADATA"}, output)
			for _, width := range []int{1, 2, 8, 20, 80} {
				rows := toolDetailRows(item, testTheme(), width)
				checkWidth(t, strings.Join(rows, "\n"), width)
				plain := ansi.Strip(strings.Join(rows, "\n"))
				if strings.Contains(plain, "HIDDEN_TAIL") || strings.Contains(plain, "HIDDEN_METADATA") {
					t.Fatal("unbounded output or metadata shown")
				}
				if len(rows) > 11 {
					t.Fatalf("preview too tall: %d", len(rows))
				}
				if width == 80 && !strings.Contains(plain, "… (truncated)") {
					t.Fatal("missing truncation marker")
				}
				if width == 80 && name == "read" && !strings.Contains(plain, "7 │ retained line") {
					t.Fatal("lost saved line offset")
				}
			}
			if item.Text != output {
				t.Fatal("preview changed saved output")
			}
		})
	}
}

func TestShortReadAndGenericCardsShrink(t *testing.T) {
	for _, name := range []string{"read", "ls", "mcp.example"} {
		item := visualItem(name, map[string]any{"path": "a.go"}, nil, "hello")
		rows := toolBody(item, testTheme(), Options{ExpandTools: true}, 120)
		if ansi.StringWidth(rows[1]) >= 40 {
			t.Fatalf("%s frame stretched: %s", name, ansi.Strip(rows[1]))
		}
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, hidden := range []string{"Parameters", "Result", "Metadata", "Read preview"} {
			if strings.Contains(plain, hidden) {
				t.Fatalf("unwanted heading %s", hidden)
			}
		}
	}
}
