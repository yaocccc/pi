package cards_test

import (
	"charm.land/lipgloss/v2"
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/render/tooldata"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender/cards"
	"strings"
	"testing"
)

func testTheme() config.Theme {
	return config.Theme{Accent: "#b7915f", Border: "#756550", Muted: "#948672", Text: "#e0d4bd", Background: "#25231f", UserBackground: "#30342b", AssistantBackground: "#292720", ToolBackground: "#292720", Success: "#a4ad81", Warning: "#d7ae72", Error: "#c77965", Thinking: "#a79b87"}
}

func checkWidth(t *testing.T, s string, width int) {
	t.Helper()
	for i, row := range strings.Split(s, "\n") {
		if n := ansi.StringWidth(row); n > width {
			t.Fatalf("line %d is %d cells, maximum %d: %q", i, n, width, row)
		}
	}
}

func blockByID(t *testing.T, d render.Document, id string) render.Block {
	t.Helper()
	for _, b := range d.Blocks {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing block %s: %+v", id, d.Blocks)
	return render.Block{}
}

func paint(s, color string) string {
	if color == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}

// Only neutral text primitives are supplied for direct adapter tests. Framing
// and dispatch assertions below exercise the real host through Transcript.
func testCards() *cards.Renderer {
	return cards.New(extensionapi.CardHost{
		Map: tooldata.DecodeMap, String: tooldata.StringField, Paint: paint, Clean: render.Clean,
		OneLine: func(s string) string { return strings.Join(strings.Fields(render.Clean(s)), " ") },
		Fit: func(s string, w int) string {
			if w <= 0 {
				return ""
			}
			return ansi.Truncate(s, w, "…")
		},
		Wrap: func(s string, w int) []string { return render.StyledLines(s, "", w) },
	})
}
func visualItem(name string, args, details any, text string) session.Item {
	a, _ := json.Marshal(args)
	d, _ := json.Marshal(details)
	return session.Item{Role: "tool", ToolName: name, Arguments: a, Details: d, Text: text, HasResult: true}
}
func visualText(item session.Item, width int) string {
	return strings.Join(testCards().QuestionRows(item, testTheme(), width), "\n")
}
