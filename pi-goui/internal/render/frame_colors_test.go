package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestRoleFramesRemainCompactAndKeepOffsets(t *testing.T) {
	items := []session.Item{{ID: "u", Role: "user", Text: "你好"}, {ID: "a", Role: "assistant", Text: "Hello"}, {ID: "s", Role: "system", Text: "Context compacted — earlier messages retained\nshort summary"}}
	doc := Transcript(items, testTheme(), Options{Width: 60})
	rows := strings.Split(doc.Text, "\n")
	roles := []string{"user", "assistant", "system"}
	if len(doc.Blocks) != 3 {
		t.Fatal("missing message ranges")
	}
	// Assistant range covers body; user/system ranges include their border.
	tops := []int{doc.Blocks[0].Start, doc.Blocks[1].Start - 1, doc.Blocks[2].Start}
	for i, kind := range roles {
		if !strings.Contains(rows[tops[i]], gradientCorner(kind, "╭")) {
			t.Fatalf("wrong %s palette: %q", kind, rows[tops[i]])
		}
	}
	userTop := strings.TrimLeft(ansi.Strip(rows[tops[0]]), " ")
	if ansi.StringWidth(userTop) != 8 || !strings.Contains(userTop, "你") {
		t.Fatalf("short user bubble grew: %q", userTop)
	}
	if !strings.HasPrefix(rows[tops[2]], gradientCorner("system", "╭")) {
		t.Fatal("System no longer left aligned")
	}
	for _, width := range []int{1, 8, 16, 60, 132} {
		checkWidth(t, Transcript(items, testTheme(), Options{Width: width}).Text, width)
	}
}
func TestGradientToolFrameRetainsCodemodeTokenTitle(t *testing.T) {
	item := session.Item{ID: "c", Role: "tool", ToolName: "codemode", HasResult: true, Error: true, EstimatedTokens: 12000, Arguments: json.RawMessage(`{"code":"text(42)"}`), Text: "failed"}
	rows := toolBody(item, testTheme(), Options{ExpandTools: true}, 60)
	if !strings.HasPrefix(rows[1], gradientCorner("codemode", "┌")) {
		t.Fatalf("codemode gradient missing: %q", rows[1])
	}
	if !strings.Contains(rows[1], tokenBadge(12000, testTheme())) {
		t.Fatal("lost red token title")
	}
	if !strings.Contains(rows[0], paint("✗", testTheme().Error)) {
		t.Fatal("lost error status")
	}
	if !strings.HasPrefix(rows[2], "\x1b[38;2;") {
		t.Fatal("tool side gradient missing")
	}
	plain := session.Item{ID: "r", Role: "tool", ToolName: "read", Text: "ok", HasResult: true}
	single := toolBody(plain, testTheme(), Options{ExpandTools: true}, 60)
	if !strings.HasPrefix(single[1], gradientCorner("tool", "╭")) {
		t.Fatal("ordinary tool gradient missing")
	}
}
