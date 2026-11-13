package cards_test

import (
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
	"strings"
	"testing"
)

func TestSemanticCommonAndUnknownTools(t *testing.T) {
	fixtures := []struct {
		item   session.Item
		wanted []string
	}{
		{visualItem("worker", map[string]any{"tasks": []any{map[string]any{"objective": "Run unit tests", "mode": "test"}}}, nil, `{"status":"completed","results":[{"task":"Unit tests","status":"passed"}]}`), []string{"✓ test", "Run unit tests"}},
		{visualItem("ask_question", map[string]any{"question": "Pick one", "options": []string{"Alpha", "Beta"}}, nil, `{"value":"Beta"}`), []string{"◦ Pick one", "○ Alpha", "✓ Beta"}},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.item.ToolName, func(t *testing.T) {
			rows, handled := testCards().Detail(fixture.item, testTheme(), 120)
			if !handled {
				t.Fatal("extension card was not handled")
			}
			text := strings.Join(rows, "\n")
			for _, want := range fixture.wanted {
				if !strings.Contains(ansi.Strip(text), want) {
					t.Fatalf("missing %q in %s", want, text)
				}
			}
			plain := ansi.Strip(text)
			if fixture.item.ToolName != "codemode" && strings.ContainsAny(plain, "{}[]") {
				t.Fatalf("protocol object dumped: %s", plain)
			}
		})
	}
}
