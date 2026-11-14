package render

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestSidebarTodoTaskTextAnimation(t *testing.T) {
	ctx := SidebarContext{Todo: []session.PlanStep{
		{ID: "1", Goal: "Running step", Status: "in_progress"},
		{ID: "2", Goal: "Waiting step", Status: "pending"},
		{ID: "3", Goal: "Finished step", Status: "completed"},
	}}
	theme := testTheme()
	theme.Dim = "#667788"
	layout := SidebarGeometry(40, ctx)
	rendered := Sidebar(session.State{}, nil, theme, 40, 40, ctx)
	if rendered != Sidebar(session.State{}, nil, theme, 40, 40, ctx) {
		t.Fatal("todo rendering must remain static")
	}
	rows := strings.Split(rendered, "\n")
	wants := []string{
		paint(" ○ 1. ", theme.Dim) + animateTodoText("Running step", ctx.TodoFrame),
		paint(" ○ 2. Waiting step", theme.Dim),
		" " + paint("✓", theme.Success) + " " + paint("3.", theme.Muted) + " " + paint("Finished step", theme.Muted),
	}
	for i, want := range wants {
		if !strings.Contains(rows[layout.TodoStart+i], want) {
			t.Fatalf("incorrect static style for step %d", i+1)
		}
	}
}

func TestSidebarTodoAnimationScope(t *testing.T) {
	for _, dim := range []string{"#667788", ""} {
		theme := testTheme()
		theme.Dim = dim
		dimColor := dim
		if dimColor == "" {
			dimColor = theme.Muted
		}
		ctx := SidebarContext{Todo: []session.PlanStep{
			{ID: "1", Goal: "Running step", Status: "in_progress"},
			{ID: "2", Goal: "Waiting step", Status: "pending"},
			{ID: "3", Goal: "Finished step", Status: "completed"},
		}}
		layout := SidebarGeometry(40, ctx)
		var previous []string
		for _, frame := range []int{5, 13} {
			ctx.TodoFrame = frame
			rows := strings.Split(Sidebar(session.State{}, nil, theme, 40, 40, ctx), "\n")
			want := paint(" ○ 1. ", dimColor) + animateTodoText("Running step", frame)
			if !strings.Contains(rows[layout.TodoStart], want) {
				t.Fatal("circle and number must stay dim while only the task text sweeps")
			}
			if previous != nil {
				if rows[layout.TodoStart] == previous[layout.TodoStart] || ansi.Strip(rows[layout.TodoStart]) != ansi.Strip(previous[layout.TodoStart]) {
					t.Fatal("running text must change colors without changing text")
				}
				for i := 1; i <= 2; i++ {
					if rows[layout.TodoStart+i] != previous[layout.TodoStart+i] {
						t.Fatal("pending and completed rows must remain static")
					}
				}
			}
			previous = rows
		}
	}
}

func TestSidebarTodoWindowCapacity(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		count, height, visible int
	}{
		{"fits ten", 10, 40, 10},
		{"fits fewer", 7, 40, 7},
		{"over ten", 12, 40, 10},
		{"short screen", 10, 27, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := SidebarContext{History: []string{"Recent"}}
			for i := 1; i <= tc.count; i++ {
				ctx.Todo = append(ctx.Todo, session.PlanStep{ID: fmt.Sprint(i), Goal: fmt.Sprintf("Step %d", i), Status: "pending"})
			}
			layout := SidebarGeometry(tc.height, ctx)
			if layout.TodoCount != tc.visible || layout.TodoOffset != tc.count-tc.visible {
				t.Fatalf("unexpected window: %+v", layout)
			}
			rows := strings.Split(ansi.Strip(Sidebar(session.State{}, nil, testTheme(), 36, tc.height, ctx)), "\n")
			window := strings.Join(rows[layout.TodoStart:layout.TodoStart+layout.TodoCount], "\n")
			if strings.Contains(window, "┃") != (tc.count > tc.visible) {
				t.Fatalf("unexpected scrollbar: %s", window)
			}
			ctx.TodoScroll = 100
			if SidebarGeometry(tc.height, ctx).TodoOffset != 0 {
				t.Fatal("scroll did not clamp to first step")
			}
		})
	}
}

func TestSidebarTodoLatestAndGeometry(t *testing.T) {
	ctx := SidebarContext{History: []string{"Recent"}, Todo: []session.PlanStep{
		{ID: "1", Goal: "First step", Status: "pending"},
		{ID: "2", Goal: "Second step", Status: "pending"},
		{ID: "3", Goal: "Third step", Status: "completed"},
		{ID: "4", Goal: "Fourth step", Status: "in_progress"},
		{ID: "5", Goal: "Fifth step", Status: "pending"},
		{ID: "6", Goal: "Sixth step", Status: "pending"},
		{ID: "7", Goal: "Seventh step", Status: "pending"},
	}}
	layout := SidebarGeometry(24, ctx)
	if layout.TodoCount != 5 || layout.TodoOffset != 2 || layout.TodoRow <= layout.SessionsStart || layout.TodoStart+layout.TodoCount != 23 {
		t.Fatalf("layout: %+v", layout)
	}
	theme := testTheme()
	for _, dim := range []string{"#667788", ""} {
		for _, warning := range []string{"#d75f00", ""} {
			theme.Dim, theme.Warning = dim, warning
			dimColor := dim
			if dimColor == "" {
				dimColor = theme.Muted
			}
			rendered := Sidebar(session.State{}, nil, theme, 36, 24, ctx)
			for _, want := range []string{
				" " + paint("✓", theme.Success) + " " + paint("3.", theme.Muted) + " " + paint("Third step", theme.Muted),
				paint(" ○ 4. ", dimColor) + animateTodoText("Fourth step", ctx.TodoFrame),
				paint(" ○ 5. Fifth step", dimColor),
			} {
				if !strings.Contains(rendered, want) {
					t.Fatalf("missing state-specific todo style: %q", want)
				}
			}
		}
	}
	plain := ansi.Strip(Sidebar(session.State{}, nil, testTheme(), 36, 24, ctx))
	for _, want := range []string{"✓ 3. Third step", "○ 4. Fourth step", "○ 5. Fifth step", "RECENTS", "┃"} {
		if !strings.Contains(plain, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(plain, "First step") {
		t.Fatal("did not default to latest")
	}
	rows := strings.Split(plain, "\n")
	if !strings.Contains(rows[layout.TodoRow], "─") || strings.Contains(plain, "TODOS") || layout.TodoStart != layout.TodoRow+1 {
		t.Fatal("todos must follow a separator immediately, without a title")
	}
	if strings.Index(rows[layout.TodoStart], "✓") != strings.Index(rows[layout.HistoryRow], "RECENTS")+1 {
		t.Fatal("todo items must have one space of extra indentation")
	}
	if layout.SessionsStart+layout.Capacity*SidebarSessionRows > layout.TodoRow {
		t.Fatal("session cards overlap todos")
	}
	ctx.TodoScroll = 100
	layout = SidebarGeometry(24, ctx)
	if layout.TodoOffset != 0 {
		t.Fatal("did not clamp earlier scroll")
	}
	plain = ansi.Strip(Sidebar(session.State{}, nil, testTheme(), 36, 24, ctx))
	if !strings.Contains(plain, "○ 1. First step") || !strings.Contains(plain, "✓ 3. Third step") || strings.Contains(plain, "Seventh step") {
		t.Fatal("wrong scrolled window")
	}
	for _, height := range []int{1, 10, 18, 24, 30, 40} {
		for _, width := range []int{1, 4, 16, 36} {
			rows := strings.Split(Sidebar(session.State{}, nil, testTheme(), width, height, ctx), "\n")
			if len(rows) != height {
				t.Fatalf("height %d: %d", height, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != width {
					t.Fatalf("width %d: %q", width, row)
				}
			}
		}
	}
	// Align single- and double-digit step numbers using the complete plan,
	// even when scrolling back to a window containing only single digits.
	ctx.Todo = append(ctx.Todo,
		session.PlanStep{Goal: "Eighth step", Status: "pending"},
		session.PlanStep{Goal: "Ninth step", Status: "pending"},
		session.PlanStep{Goal: "Tenth step", Status: "pending"},
	)
	ctx.TodoScroll = 0
	plain = ansi.Strip(Sidebar(session.State{}, nil, theme, 36, 24, ctx))
	for _, want := range []string{" ○  9. Ninth step", " ○ 10. Tenth step"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing aligned step: %q", want)
		}
	}
	ctx.TodoScroll = 100
	plain = ansi.Strip(Sidebar(session.State{}, nil, theme, 36, 24, ctx))
	if !strings.Contains(plain, " ○  1. First step") {
		t.Fatal("scrolling must preserve full-plan number alignment")
	}
	ctx.Todo = nil
	if SidebarGeometry(40, ctx).TodoCount != 0 || strings.Contains(ansi.Strip(Sidebar(session.State{}, nil, testTheme(), 36, 40, ctx)), "TODOS") {
		t.Fatal("empty todo should hide")
	}
}

func TestTodoSweepMatchesWorkingParameters(t *testing.T) {
	if todoSweepPeriod != 15 {
		t.Fatal("todo must share the working message's 15-frame period")
	}
	colors := regexp.MustCompile(`\x1b\[38;2;(\d+);\d+;\d+m`)
	for _, width := range []int{1, 12, 40, 100} {
		content := strings.Repeat("x", width)
		for frame := 0; frame < 30; frame++ {
			// Same phase, travel, sigma, strength and cutoff as animateForeground.
			phase := (frame % 15) * 20 / 15
			center := -2 + float64(width+3)*float64(phase)/16
			for col, rgb := range colors.FindAllStringSubmatch(animateTodoText(content, frame), -1) {
				want := 136
				if phase < 17 {
					distance := (float64(col) - center) / 2.0
					blend := math.Exp(-0.5*distance*distance) * 0.9
					if blend > 0.01 {
						want = int(math.Round(136 + (190-136)*blend))
					}
				}
				level, _ := strconv.Atoi(rgb[1])
				if level != want {
					t.Fatalf("working sweep mismatch: width=%d frame=%d column=%d got=%d want=%d", width, frame, col, level, want)
				}
			}
		}
	}
	// The entering highlight begins at the number; the caller excludes the icon.
	prefix := colors.FindAllStringSubmatch(animateTodoText("1. Running step", 2), -1)
	level, _ := strconv.Atoi(prefix[0][1])
	if level <= 136 {
		t.Fatal("number must participate in the sweep")
	}
}

func TestTodoTextNeutralSweep(t *testing.T) {
	colors := regexp.MustCompile(`\x1b\[38;2;(\d+);(\d+);(\d+)m`)
	for _, content := range []string{"", "3. 检查 👩‍💻 é", "12. Verify changes"} {
		previous := ""
		changed := false
		for frame := 0; frame < todoSweepPeriod; frame++ {
			got := animateTodoText(content, frame)
			if ansi.Strip(got) != content || ansi.StringWidth(got) != ansi.StringWidth(content) {
				t.Fatal("animation changed text or width")
			}
			if got != animateTodoText(content, frame+todoSweepPeriod) {
				t.Fatal("sweep must repeat at its configured period")
			}
			for _, rgb := range colors.FindAllStringSubmatch(got, -1) {
				level, _ := strconv.Atoi(rgb[1])
				if rgb[1] != rgb[2] || rgb[2] != rgb[3] || level < 136 || level > 190 {
					t.Fatalf("expected neutral gray to soft white: %v", rgb)
				}
				if (frame%15)*20/15 >= 17 && level != 136 {
					t.Fatal("pause should return to neutral gray")
				}
			}
			if frame > 0 && got != previous {
				changed = true
			}
			previous = got
		}
		if content != "" && !changed {
			t.Fatal("text must animate")
		}
	}
}
