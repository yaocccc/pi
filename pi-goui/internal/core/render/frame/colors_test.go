package frame

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strconv"
	"strings"
	"testing"
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
			if got := Border(kind, edges[i], "│"); got != want {
				t.Fatalf("%s/%s = %q want %q", kind, edges[i], got, want)
			}
			if kind == "input" && Border("system", edges[i], "│") != want {
				t.Fatal("System must reuse reference purple")
			}
		}
	}
	if Border("unknown", "unknown", "│") != Border("tool", "top", "│") {
		t.Fatal("unsafe fallback palette")
	}
}
func TestFrameBoxKeepsContentGeometryAndTransparency(t *testing.T) {
	for _, width := range []int{1, 2, 3, 4, 8, 16, 60, 96, 128} {
		body := []string{"你好 é 👩‍💻", "", "\x1b[38;2;200;200;200mgpt-6-astra · high\x1b[39m"}
		rows := Box("input", body, width)
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
		if !strings.HasPrefix(rows[0], Border("input", "topLeft", "┌")) {
			t.Fatal("wrong input top-left")
		}
		if width > 1 && !strings.HasSuffix(rows[len(rows)-1], Border("input", "topLeft", "┘")) {
			t.Fatal("wrong input bottom-right")
		}
		if width >= 60 && !strings.Contains(rows[3], body[2]) {
			t.Fatal("footer normal foreground was altered")
		}
	}
}
