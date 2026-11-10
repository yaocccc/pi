package render

import (
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
	"strings"
	"testing"
)

func TestSidebarHistoryHoverPaint(t *testing.T) {
	theme, err := config.LoadTheme("", "pi")
	if err != nil {
		t.Fatal(err)
	}
	ctx := SidebarContext{History: []string{"hovered", "plain"}, HistoryHovered: []bool{true, false}}
	layout := SidebarGeometry(36, ctx)
	rows := strings.Split(Sidebar(session.State{}, nil, theme, 32, 36, ctx), "\n")
	for i := 0; i < 2; i++ {
		if strings.Contains(rows[layout.HistoryStart+i], "\x1b[48;") {
			t.Fatalf("unexpected background on row %d", i)
		}
		highlight := strings.Contains(rows[layout.HistoryStart+i], "38;2;"+workbenchAccent)
		if highlight != (i == 0) {
			t.Fatalf("unexpected hover foreground on row %d", i)
		}
	}
}

func TestSidebarHistoryViewport(t *testing.T) {
	theme, err := config.LoadTheme("", "pi")
	if err != nil {
		t.Fatal(err)
	}
	ctx := SidebarContext{History: []string{"first", "second", "third", "fourth", "fifth", "sixth long session name"}, HistoryOffset: 99, HistoryHovered: []bool{false, false, false, false, false, true}}
	for _, width := range []int{1, 4, 12, 32} {
		layout := SidebarGeometry(36, ctx)
		if layout.HistoryOffset != 1 || layout.HistoryCount != 5 {
			t.Fatal("offset not clamped")
		}
		raw := Sidebar(session.State{}, nil, theme, width, 36, ctx)
		rows := strings.Split(ansi.Strip(raw), "\n")
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatalf("width %d: %q", width, row)
			}
		}
		if width == 32 {
			if !strings.Contains(rows[layout.HistoryStart], "  second") || !strings.Contains(rows[layout.HistoryStart+4*SidebarHistoryRows], "  sixth") || strings.Contains(ansi.Strip(raw), "first") {
				t.Fatal("viewport ordinal or offset mismatch")
			}
			if !strings.Contains(rows[layout.HistoryStart], "│") || !strings.Contains(rows[layout.HistoryStart+4*SidebarHistoryRows], "┃") {
				t.Fatal("scrollbar does not indicate bottom position")
			}
		}
		if width == 12 && !strings.Contains(rows[layout.HistoryStart+4*SidebarHistoryRows], "…") {
			t.Fatal("long history name not truncated")
		}
	}
}

func TestSidebarHistoryGeometry(t *testing.T) {
	theme, err := config.LoadTheme("", "pi")
	if err != nil {
		t.Fatal(err)
	}
	for _, quota := range []*SidebarQuota{nil, {}} {
		for _, height := range []int{1, 12, 17, 20, 24, 36, 38} {
			ctx := SidebarContext{Quota: quota, History: []string{"recent", "second", "third", "fourth", "fifth", "sixth"}}
			layout := SidebarGeometry(height, ctx)
			rows := strings.Split(ansi.Strip(Sidebar(session.State{}, []SessionRow{{Name: "open card"}}, theme, 32, height, ctx)), "\n")
			if len(rows) != height {
				t.Fatalf("height %d: %d rows", height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != 32 {
					t.Fatalf("bad width: %q", row)
				}
			}
			if height == 38 && layout.HistoryCount != 5 {
				t.Fatal("expected five recent sessions")
			}
			if strings.Contains(strings.Join(rows, "\n"), "sixth") {
				t.Fatal("sixth history row rendered")
			}
			if layout.NewRow < height-1 {
				if !strings.Contains(rows[layout.HistoryRow], "RECENTS") || !strings.Contains(rows[layout.NewRow], "+ New") {
					t.Fatal("geometry differs from rendering")
				}
				for i := 0; i < layout.HistoryCount; i++ {
					if !strings.Contains(rows[layout.HistoryStart+i*SidebarHistoryRows], ctx.History[i]) {
						t.Fatal("history row mismatch")
					}
				}
				if layout.NewRow != layout.HistoryStart+layout.HistoryCount*SidebarHistoryRows+1 || (layout.SessionsStart-1 < height-1 && strings.Contains(rows[layout.SessionsStart-1], "──")) {
					t.Fatal("new session must follow history with spacing and no lower separator")
				}
			}
			if layout.Capacity > 0 && !strings.Contains(rows[layout.SessionsStart+1], "open card") {
				t.Fatal("open card displaced")
			}
		}
	}
}
