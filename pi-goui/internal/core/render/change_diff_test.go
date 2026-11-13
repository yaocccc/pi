package render

import (
	"fmt"
	"strings"

	"pi-goui/internal/core/session"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestChangeContentWidthAndSingleLine(t *testing.T) {
	long := strings.Repeat("中文👩‍💻 abc ", 60) + "HIDDEN-END"
	for _, width := range []int{1, 4, 20, 70, 100, 240} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			rows := writtenGrid(long, "file.txt", width, testTheme())
			if len(rows) != 2 {
				t.Fatalf("one source line wrapped into %d rows", len(rows))
			}
			checkWidth(t, strings.Join(rows, "\n"), min(width, 100))
			if strings.Contains(strings.Join(rows, "\n"), "HIDDEN-END") {
				t.Fatal("long line tail was not truncated")
			}
			for _, size := range []int{width, max(width, 72)} {
				short := changeGrid("-1 short\n+1 new", "file.go", size, testTheme(), true)
				longRows := changeGrid("-1 "+long+"\n+1 "+long, "file.go", size, testTheme(), true)
				if len(longRows) != len(short) {
					t.Fatal("diff source lines wrapped")
				}
				checkWidth(t, strings.Join(longRows, "\n"), min(size, 100))
			}
			for _, name := range []string{"write", "edit"} {
				item := visualItem(name, map[string]any{"path": strings.Repeat("directory/", 40) + "file.go", "content": long, "oldText": long, "newText": long}, nil, "done")
				body := toolBody(item, testTheme(), Options{ExpandTools: true}, width)
				checkWidth(t, strings.Join(body, "\n"), min(width, 104))
				detail := toolDetailRows(item, testTheme(), width)
				checkWidth(t, strings.Join(detail, "\n"), min(width, 100))
			}
		})
	}
	styled := &changeLine{kind: '+', styled: paint(long, testTheme().Success)}
	rows := changeCell(styled, 1, 40, 1, "", testTheme())
	if len(rows) != 1 || ansi.StringWidth(rows[0]) > 40 {
		t.Fatal("styled wide-character code must truncate safely")
	}
}

func TestWriteTailFold(t *testing.T) {
	var lines []string
	for i := 1; i <= 15; i++ {
		lines = append(lines, fmt.Sprintf("unique-line-%02d", i))
	}
	item := visualItem("write", map[string]any{"path": "file.txt", "content": strings.Join(lines, "\n") + "\n"}, nil, "done")
	for _, width := range []int{1, 4, 20, 100} {
		opts := Options{Width: width, ExpandTools: true}
		rows := strings.Join(toolBody(item, testTheme(), opts, width), "\n")
		checkWidth(t, rows, width)
		if width == 100 {
			mustContainVisual(t, rows, "5 earlier lines hidden", "6 │ unique-line-06", "15 │ unique-line-15", "╭─ write")
			if strings.Contains(rows, "unique-line-01") {
				t.Fatal("head not folded")
			}
		}
		doc := Transcript([]session.Item{item}, testTheme(), opts)
		found := false
		for _, b := range doc.Blocks {
			if b.ID == item.ID && b.Foldable && b.Expanded {
				found = true
			}
			if b.Role == "writeContent" || b.ID == item.ID+":write-content" {
				t.Fatal("unexpected nested content fold")
			}
		}
		if !found {
			t.Fatal("missing expanded tool block")
		}
	}
	collapsed := strings.Join(toolBody(item, testTheme(), Options{}, 100), "\n")
	if strings.Contains(collapsed, "unique-line-15") {
		t.Fatal("collapsed tool displays content")
	}
}

func TestChangeGridLayouts(t *testing.T) {
	item := visualItem("edit", map[string]any{"path": "file.ts"}, map[string]any{"diff": "@@ -10,2 +10,3 @@\n-old value\n+new value\n+extra value\n context"}, "done")
	wide := strings.Join(toolDetailRows(item, testTheme(), 100), "\n")
	mustContainVisual(t, wide, "split", "old", "new", "10 │ old value", "10 │ new value", "11 │ extra value", "12 │ context")
	if !strings.Contains(wide, paint("▌ 10", testTheme().Error)) || !strings.Contains(wide, paint("▌ 10", testTheme().Success)) {
		t.Fatal("missing change rail colors")
	}
	narrow := strings.Join(toolDetailRows(item, testTheme(), 40), "\n")
	mustContainVisual(t, narrow, "unified", "10 │ old value", "10 │ new value")
	for _, width := range []int{1, 2, 4, 8, 20, 71, 72, 100} {
		checkWidth(t, strings.Join(toolDetailRows(item, testTheme(), width), "\n"), width)
	}
	preview := visualItem("edit", map[string]any{"oldText": "123 old\n-- marker", "newText": "456 new\n++ marker"}, nil, "done")
	mustContainVisual(t, strings.Join(toolDetailRows(preview, testTheme(), 100), "\n"), "123 old", "456 new", "-- marker", "++ marker", "not a recorded diff")
}

func TestWrittenGridWrappingAndSafety(t *testing.T) {
	item := visualItem("write", map[string]any{"path": "file.ts", "content": "const value = `中文👩‍💻long value`;\n\x1b[2Jtail\n"}, nil, "done")
	for _, width := range []int{1, 2, 4, 8, 20, 80} {
		rows := strings.Join(toolDetailRows(item, testTheme(), width), "\n")
		checkWidth(t, rows, width)
		if strings.Contains(rows, "\x1b[2J") {
			t.Fatal("unsafe control sequence")
		}
	}
	wide := strings.Join(toolDetailRows(item, testTheme(), 80), "\n")
	mustContainVisual(t, wide, "↳ written", "1 │ const value", "2 │ tail")
	if !strings.Contains(wide, paint("▌ 1", testTheme().Success)) {
		t.Fatal("missing addition rail")
	}
	item.Error = true
	item.Text = "Permission denied"
	failed := strings.Join(toolDetailRows(item, testTheme(), 80), "\n")
	mustContainVisual(t, failed, "Change failed", "Permission denied")
	if strings.Contains(failed, "written") || strings.Contains(failed, "const value") {
		t.Fatal("failed write shown as applied")
	}
}

func TestChangeCardsShowOnlyRecordedDiff(t *testing.T) {
	for _, name := range []string{"edit", "write"} {
		item := visualItem(name, map[string]any{"path": "file.go", "content": "not authoritative", "oldText": "before", "newText": "after"}, map[string]any{"diff": "-old\n+new", "firstChangedLine": 1}, "Operation completed")
		plain := ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
		if !strings.Contains(plain, "│ old") || !strings.Contains(plain, "│ new") || strings.Contains(plain, "not authoritative") || strings.Contains(plain, "before") {
			t.Fatalf("%s card ignored recorded diff: %s", name, plain)
		}
		for _, width := range []int{1, 2, 4, 8, 20, 80} {
			checkWidth(t, strings.Join(toolBody(item, testTheme(), Options{ExpandTools: true}, width), "\n"), width)
		}
		item.Error = true
		item.Text = "Permission denied"
		plain = ansi.Strip(strings.Join(toolDetailRows(item, testTheme(), 80), "\n"))
		if !strings.Contains(plain, "Permission denied") || strings.Contains(plain, "+new") {
			t.Fatal("failed operation shown as applied diff")
		}
	}
}
