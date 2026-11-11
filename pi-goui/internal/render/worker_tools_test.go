package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWorkerToolsRecentThreeWithoutFold(t *testing.T) {
	card := workerCard{toolCalls: 5}
	for i := range 5 {
		status := "completed"
		if i == 3 {
			status = "failed"
		}
		if i == 4 {
			status = "running"
		}
		card.activities = append(card.activities, map[string]any{"type": "tool", "label": fmt.Sprintf("call%d", i), "detail": "path 中文", "status": status})
		card.activities = append(card.activities, map[string]any{"type": "phase", "detail": "not a step"})
	}
	rows := workerToolRows(card, testTheme(), 100)
	plain := ansi.Strip(strings.Join(rows, "\n"))
	if ansi.Strip(rows[0]) != "  ≡ Activity" {
		t.Fatal("worker header includes outcome counts")
	}
	for _, want := range []string{"≡ Activity", "2 items (已折叠)", "✓ call2 path 中文", "✗ call3 path 中文", "⋯ call4 path 中文", "(失败)", "(运行中…)"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %s: %s", want, plain)
		}
	}
	for _, hidden := range []string{"call0", "call1", "not a step", "tokens", "展开", "│", "•", "已记录"} {
		if strings.Contains(plain, hidden) {
			t.Fatalf("unexpected %s: %s", hidden, plain)
		}
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "  ≡") {
		t.Fatal("wrong header indent")
	}
	for _, row := range rows[1:] {
		if !strings.HasPrefix(ansi.Strip(row), "    ") {
			t.Fatal("tool rows need an extra indent")
		}
	}
	if len(rows) != 5 {
		t.Fatalf("expected header, omission row, and three calls: %v", rows)
	}
	for _, width := range []int{1, 2, 4, 8, 20, 40, 100} {
		checkWidth(t, strings.Join(workerToolRows(card, testTheme(), width), "\n"), width)
	}
	card.toolCalls = 12
	plain = ansi.Strip(strings.Join(workerToolRows(card, testTheme(), 100), "\n"))
	if !strings.Contains(plain, "≡ Activity") || !strings.Contains(plain, "9 items (已折叠)") {
		t.Fatal("invented outcomes for unrecorded calls: " + plain)
	}
}

func TestWorkerThinkingMergedIntoRecentSteps(t *testing.T) {
	card := workerCard{toolCalls: 2, activities: []any{
		map[string]any{"type": "thinking", "detail": "**Earlier thought**"},
		map[string]any{"type": "tool", "label": "read", "detail": "old.go", "status": "completed"},
		map[string]any{"type": "tool", "label": "edit", "detail": "new.go", "status": "completed"},
		map[string]any{"type": "thinking", "detail": "**Implementing config fallback** **Refining mode popup**"},
	}}
	rows := workerToolRows(card, testTheme(), 100)
	want := []string{"  ≡ Activity", "    … 2 items (已折叠)", "    ✓ edit new.go", "    ! Implementing config fallback", "    ! Refining mode popup"}
	if len(rows) != len(want) {
		t.Fatalf("expected only latest three steps: %v", rows)
	}
	for i, row := range rows {
		if ansi.Strip(row) != want[i] {
			t.Fatalf("row %d = %q, want %q", i, ansi.Strip(row), want[i])
		}
	}
	for _, width := range []int{1, 2, 4, 8, 20, 80} {
		checkWidth(t, strings.Join(workerToolRows(card, testTheme(), width), "\n"), width)
	}
	for _, text := range []string{"**Partial thought", "First line\nSecond line", "**标题一** 正文 **标题二**", "\x1b[2J**safe**"} {
		steps := workerThinkingSteps(text)
		if len(steps) == 0 {
			t.Fatal("lost thinking text")
		}
		for _, step := range steps {
			if strings.Contains(step, "**") || strings.ContainsAny(step, "\x1b\n") {
				t.Fatal("markup/control leaked")
			}
		}
	}
}

func TestWorkerToolsMissingActivities(t *testing.T) {
	if len(workerToolRows(workerCard{}, testTheme(), 80)) != 0 {
		t.Fatal("invented tool calls")
	}
	rows := workerToolRows(workerCard{toolCalls: 4}, testTheme(), 80)
	plain := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(plain, "≡ Activity") || strings.Contains(plain, "✓") || strings.Contains(plain, "✗") {
		t.Fatal("fabricated outcomes without activity data")
	}
}
