package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBashCompactCard(t *testing.T) {
	command := "git -C .worker-question-demo status --short"
	item := visualItem("bash", map[string]any{"command": command, "timeout": 10}, nil, "")
	for _, width := range []int{80, 160} {
		rows := toolBody(item, testTheme(), Options{ExpandTools: true}, width)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		for _, want := range []string{command + " (Timeout: 10)", "(no output)"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q: %s", want, plain)
			}
		}
		for _, bad := range []string{"1 │", "Parameters", "Result"} {
			if strings.Contains(plain, bad) {
				t.Fatalf("unwanted %q: %s", bad, plain)
			}
		}
		if ansi.StringWidth(rows[1]) != ansi.StringWidth(command+" (Timeout: 10)")+4 {
			t.Fatal("bash frame did not shrink to command width")
		}
	}
	detail := bashDetailRows(item, testTheme(), 100)
	if !strings.Contains(detail[0], paint("(Timeout: 10)", visualDim(testTheme()))) {
		t.Fatal("parameters are not dim-colored on the command row")
	}
	for _, width := range []int{1, 8, 20, 40} {
		checkWidth(t, strings.Join(bashDetailRows(item, testTheme(), width), "\n"), width)
	}
	item = visualItem("bash", map[string]any{"command": "pwd"}, nil, "")
	plain := ansi.Strip(strings.Join(bashDetailRows(item, testTheme(), 80), "\n"))
	if plain != "pwd\n\n(no output)" {
		t.Fatal(plain)
	}
}

func TestBashOutputHeadLimit(t *testing.T) {
	for _, output := range []string{strings.Repeat("line\n", 20) + "HIDDEN_TAIL", strings.Repeat("界", 300) + "HIDDEN_TAIL"} {
		item := visualItem("bash", map[string]any{"command": "echo test"}, nil, output)
		rows := bashDetailRows(item, testTheme(), 30)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		if strings.Contains(plain, "HIDDEN_TAIL") || !strings.Contains(plain, "… (truncated)") {
			t.Fatal(plain)
		}
		if len(rows) > 2+bashOutputRows+1 {
			t.Fatalf("too many output rows: %d", len(rows))
		}
		if item.Text != output {
			t.Fatal("saved output was changed")
		}
	}
	item := visualItem("bash", map[string]any{"command": "echo test"}, nil, strings.Repeat("line\n", bashOutputRows))
	if strings.Contains(strings.Join(bashDetailRows(item, testTheme(), 30), "\n"), "truncated") {
		t.Fatal("exactly eight lines marked truncated")
	}
	for _, width := range []int{1, 2, 4, 8, 20, 40} {
		checkWidth(t, strings.Join(toolBody(item, testTheme(), Options{ExpandTools: true}, width), "\n"), width)
	}
}
