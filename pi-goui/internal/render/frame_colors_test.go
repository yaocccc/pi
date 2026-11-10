package render

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/session"
)

func TestFrameReferenceEdgePalettes(t *testing.T) {
	expected := map[string][]string{
		"user":      {"68d3ae", "6ed8b4", "79dcbc", "87dec3", "7bd4b8", "6ccdaf", "5fc6a5", "62ccaa"},
		"assistant": {"78b5ef", "80bff5", "8ac7f6", "91c8ef", "85bbe8", "77afe0", "6ea6dc", "71afe6"},
		"input":     {"b6a0e8", "bca8ee", "c4b0f2", "c8b5ef", "bda9e7", "b19ddd", "a892d8", "ad97df"},
		"tool":      {"71899f", "71899f", "71899f", "71899f", "71899f", "71899f", "71899f", "71899f"},
	}
	edges := []string{"topLeft", "top", "topRight", "right", "bottomRight", "bottom", "bottomLeft", "left"}
	for kind, colors := range expected {
		for i, hex := range colors {
			rgb, _ := strconv.ParseUint(hex, 16, 24)
			want := fmt.Sprintf("\x1b[38;2;%d;%d;%dm│\x1b[39m", rgb>>16, (rgb>>8)&255, rgb&255)
			if got := FrameBorder(kind, edges[i], "│"); got != want {
				t.Fatalf("%s/%s = %q want %q", kind, edges[i], got, want)
			}
			if kind == "input" && FrameBorder("system", edges[i], "│") != want {
				t.Fatal("System must reuse reference purple")
			}
		}
	}
	if FrameBorder("unknown", "unknown", "│") != FrameBorder("tool", "top", "│") {
		t.Fatal("unsafe fallback palette")
	}
}
func TestFrameBoxKeepsContentGeometryAndTransparency(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 8, 16, 60, 96, 128} {
		body := []string{"你好 é 👩‍💻", "", "\x1b[38;2;200;200;200mgpt-6-astra · high\x1b[39m"}
		rows := FrameBox("input", body, width)
		if len(rows) != len(body)+2 {
			t.Fatal("frame changed editor height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatalf("width %d row %q", width, row)
			}
			if strings.Contains(row, "\x1b[48;") || strings.Contains(row, "\x1b[0m") {
				t.Fatal("frame painted background or reset body attributes")
			}
		}
		if !strings.HasPrefix(rows[0], FrameBorder("input", "topLeft", "┌")) {
			t.Fatal("wrong input top-left")
		}
		if width > 1 && !strings.HasSuffix(rows[len(rows)-1], FrameBorder("input", "topLeft", "┘")) {
			t.Fatal("wrong input bottom-right")
		}
		if width >= 60 && !strings.Contains(rows[3], body[2]) {
			t.Fatal("footer normal foreground was altered")
		}
	}
}
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
