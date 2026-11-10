package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSidebarSessionCardsWorkbenchPalette(t *testing.T) {
	sessions := []SessionRow{{ID: "1", Name: "当前会话", Status: "input +2", Active: true}, {ID: "2", Name: "other", Status: "idle"}}
	rows := sidebarSessionCards(sessions, 28, 5)
	if len(rows) != 4 {
		t.Fatalf("expected two complete cards, got %d rows", len(rows))
	}
	for _, want := range []string{"› 1 等待回答 +2", "  当前会话", "  2 idle", "  other"} {
		if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), want) {
			t.Errorf("missing %q in %#v", want, rows)
		}
	}
	for _, want := range []string{"38;2;183;151;229", "38;2;200;200;204", "38;2;221;190;112", "38;2;116;192;133", "\x1b[1m"} {
		if !strings.Contains(strings.Join(rows, "\n"), want) {
			t.Errorf("missing style %q", want)
		}
	}
	for i, row := range append(rows, SessionSectionTitle(), NewSessionLabel()) {
		if i < 2 && (!strings.HasPrefix(row, "\x1b[48;2;82;85;88m") || !strings.HasSuffix(row, "\x1b[49m")) {
			t.Fatal("selected row must fill and reset its background")
		}
		if (i >= 2 && strings.Contains(row, "\x1b[48;")) || strings.Contains(row, "\x1b[0m") {
			t.Fatalf("background/global reset: %q", row)
		}
	}
	if !strings.Contains(SessionSectionTitle(), "38;2;200;200;204") || !strings.Contains(NewSessionLabel(), "38;2;183;151;229") {
		t.Fatal("section labels do not match workbench palette")
	}
}

func TestSidebarSessionCapacityAndWidth(t *testing.T) {
	for _, tc := range []struct{ height, want int }{{0, 0}, {12, 0}, {14, 0}, {15, 1}, {16, 1}, {17, 2}, {18, 2}, {19, 3}, {36, 11}} {
		if got := SidebarSessionCapacity(tc.height); got != tc.want {
			t.Errorf("capacity(%d)=%d, want %d", tc.height, got, tc.want)
		}
	}
	sessions := []SessionRow{{ID: "1", Name: "长名称 👩‍💻 session", Status: "working +12", Active: true}, {ID: "2", Name: "other", Status: "error"}}
	for _, width := range []int{1, 12, 28} {
		for _, height := range []int{0, 1, 2, 3, 4, 5} {
			rows := sidebarSessionCards(sessions, width, height)
			if len(rows) != min(len(sessions), height/2)*2 {
				t.Fatalf("width=%d height=%d: unexpected rows %d", width, height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("width %d exceeded: %q", width, row)
				}
			}
		}
	}
	if got := ansi.Strip(sidebarSessionCards(sessions, 28, 2)[0]); !strings.Contains(got, "working +12") {
		t.Fatalf("lost status or unread count: %q", got)
	}
}

func TestWorkbenchSessionStatusColors(t *testing.T) {
	for _, tc := range []struct{ state, label, color string }{
		{"idle", "idle", "116;192;133"}, {"working", "working", "183;151;229"},
		{"blocked", "等待回答", "221;190;112"}, {"input", "等待回答", "221;190;112"},
		{"done", "done", "113;181;166"}, {"error", "error", "218;126;126"},
		{"starting", "starting", "153;158;163"}, {"closing", "closing", "153;158;163"}, {"stopped", "stopped", "153;158;163"},
	} {
		label, color := workbenchStatus(tc.state + " +3")
		if label != tc.label+" +3" || color != tc.color {
			t.Errorf("%s: got %q %q", tc.state, label, color)
		}
	}
}
