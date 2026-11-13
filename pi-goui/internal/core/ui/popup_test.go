package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

func popupOptions(n int) []option {
	out := make([]option, n)
	for i := range out {
		out[i] = option{Label: fmt.Sprintf("Option %02d", i), Value: fmt.Sprintf("value-%02d", i), Detail: "description"}
	}
	return out
}
func TestPopupBackdropSliceWideEdges(t *testing.T) {
	for _, glyph := range []string{"中", "🙂", "👩‍💻"} {
		for _, styled := range []bool{false, true} {
			s := "a" + glyph + "bc" + glyph + "d"
			if styled {
				s = "\x1b[31m" + s + "\x1b[0m"
			}
			for _, tc := range []struct {
				left, right int
				want        string
			}{
				{0, 2, "a "},
				{2, 8, " bc" + glyph + "d"},
				{2, 6, " bc "},
				{2, 2, ""},
				{7, 10, "d  "},
			} {
				got := popupBackdropSlice(s, tc.left, tc.right)
				if ansi.Strip(got) != tc.want || ansi.StringWidth(got) != tc.right-tc.left {
					t.Fatalf("slice(%q, %d, %d) = %q, want %q", s, tc.left, tc.right, got, tc.want)
				}
			}
		}
	}
}

func TestPopupCompactBackdropAndSharedHitGeometry(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	tab := m.current()
	tab.setValue("keep draft")
	m.resizeEditor(tab)
	tab.state.Items = []session.Item{{ID: "a", Role: "assistant", Text: "background"}}
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("BACKGROUND-%02d ", i)+strings.Repeat("x", m.mainWidth()))
	}
	tab.doc = render.Document{Text: strings.Join(lines, "\n")}
	tab.dirty = false
	tab.offset = 4
	tab.follow = false
	before := m.View().Content
	focused := tab.editor.Area.Focused()
	d := m.newDialog("model", "Choose model", "Type a filter")
	d.options = popupOptions(30)
	d.selected = 15
	tab.dialogs = []*dialog{d}
	p := m.popupGeometry(d)
	after := m.View().Content
	if p.y+p.height != m.editorTop(tab) {
		t.Fatalf("popup not touching input top: %+v input=%d", p, m.editorTop(tab))
	}
	if p.width > 72 || p.width >= m.mainWidth() || p.count > 9 || p.height >= m.height {
		t.Fatalf("not a compact popup: %+v", p)
	}
	if strings.Split(before, "\n")[0] != strings.Split(after, "\n")[0] {
		t.Fatal("chat backdrop replaced")
	}
	if tab.offset != 4 || tab.follow || tab.editor.Value() != "keep draft" || tab.editor.Area.Focused() != focused {
		t.Fatal("popup paint mutated scroll/draft/focus")
	}
	if !strings.Contains(after, "keep draft") {
		t.Fatal("input backdrop hidden")
	}
	for n := 0; n < p.count; n++ {
		row := ansi.Strip(strings.Split(after, "\n")[p.optionY+n])
		if !strings.Contains(row, d.options[p.start+n].Label) {
			t.Fatalf("painted option mismatch: %q", row)
		}
		index, ok := m.popupOptionAt(d, p.x+3, p.optionY+n)
		if !ok || index != p.start+n {
			t.Fatal("hit geometry differs from painting")
		}
	}
	m.mouse(tea.Mouse{Button: tea.MouseLeft, X: 0, Y: 0})
	if len(tab.dialogs) != 1 || tab.offset != 4 {
		t.Fatal("outside click affected background")
	}
}
func TestPopupMouseRespondsToCorrectOption(t *testing.T) {
	m := testModel(t, "")
	c, path := fakeClient(t)
	tab := m.current()
	tab.client = c
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 36})
	d := m.newDialog("select", "Worker settings", "Choose an option")
	d.requestID = "worker-settings"
	d.options = popupOptions(25)
	d.selected = 16
	tab.dialogs = []*dialog{d}
	p := m.popupGeometry(d)
	target := p.start + 2
	commandResult(m.mouse(tea.Mouse{Button: tea.MouseLeft, X: p.x + 3, Y: p.optionY + 2}))
	records := wireRecords(t, path, 1)
	if records[0]["type"] != "extension_ui_response" || records[0]["id"] != "worker-settings" || records[0]["value"] != d.options[target].Value {
		t.Fatalf("incorrect response: %v", records)
	}
	if len(tab.dialogs) != 0 {
		t.Fatal("confirmed popup retained")
	}
}
func TestPopupFilteringAndHelpScrollUseVisibleWindow(t *testing.T) {
	m := testModel(t, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tab := m.current()
	d := m.newDialog("select", "Settings", "")
	d.options = popupOptions(30)
	tab.dialogs = []*dialog{d}
	press(m, '2', 0, "2")
	press(m, '9', 0, "9")
	if got := m.filtered(d); len(got) != 1 || got[0].Value != "value-29" {
		t.Fatalf("filter did not apply: %v", got)
	}
	if !strings.Contains(m.View().Content, "Option 29") {
		t.Fatal("filtered option not visible")
	}
	help := m.newDialog("help", "Hotkeys", "")
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("help line %02d", i))
	}
	help.editor.SetValue(strings.Join(lines, "\n"))
	tab.dialogs = []*dialog{help}
	for i := 0; i < 20; i++ {
		m.dialogKey(tab, tea.KeyPressMsg{}, "pagedown")
	}
	p := m.popupGeometry(help)
	if p.start+p.count != 50 || !strings.Contains(m.View().Content, "help line 49") {
		t.Fatalf("help cannot reach bottom: %+v", p)
	}
}
func TestPopupSizesAndUnsafePluginText(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	for _, size := range [][2]int{{132, 36}, {100, 28}, {80, 24}, {60, 20}, {20, 8}, {5, 4}, {1, 1}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, kind := range []string{"model", "select", "session", "confirm", "close", "resume", "input", "editor", "help"} {
			d := m.newDialog(kind, "Title \x1b]52;c;evil\a 中文", "Long message "+strings.Repeat("wide 👩‍💻 ", 30))
			d.options = popupOptions(15)
			d.options[0].Label = "Option \x1b[2J 中文"
			tab.dialogs = []*dialog{d}
			if kind == "editor" || kind == "help" {
				d.editor.SetValue(strings.Repeat("line\n", 30))
			}
			p := m.popupGeometry(d)
			rows := m.popupRows(d, p)
			if p.x < 0 || p.y < 0 || p.x+p.width > m.mainWidth() || p.y+p.height > m.height || len(rows) != p.height {
				t.Fatalf("%v/%s invalid rectangle: %+v rows=%d", size, kind, p, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != p.width {
					t.Fatalf("%v/%s row width=%d want%d", size, kind, ansi.StringWidth(row), p.width)
				}
			}
			text := m.View().Content
			if strings.Contains(text, "\x1b]52") || strings.Contains(text, "\x1b[2J") {
				t.Fatal("plugin control escaped")
			}
			if len(strings.Split(text, "\n")) != m.height {
				t.Fatal("popup changed terminal height")
			}
			for _, row := range strings.Split(text, "\n") {
				if ansi.StringWidth(row) > m.width {
					t.Fatal("popup overflowed terminal")
				}
			}
		}
	}
}
func TestPopupAnchorsAboveGrowingEditorAndBeforeTabCreation(t *testing.T) {
	m := testModel(t, "")
	for _, height := range []int{36, 20, 9, 5, 1} {
		m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		tab := m.current()
		tab.setValue(strings.Repeat("draft line\n", 8))
		m.resizeEditor(tab)
		for _, kind := range []string{"model", "session", "session-rename", "session-delete", "input", "editor", "confirm", "help"} {
			d := m.newDialog(kind, "Dialog", "")
			d.options = popupOptions(20)
			p := m.popupGeometry(d)
			if p.y+p.height != max(0, m.editorTop(tab)) {
				t.Fatalf("%s height=%d: popup=%+v input=%d", kind, height, p, m.editorTop(tab))
			}
			if len(m.popupRows(d, p)) != p.height {
				t.Fatal("zero-height or tiny popup rows mismatch")
			}
		}
	}
	m.tabs = nil
	d := m.newDialog("session-delete", "Delete?", "")
	if p := m.popupGeometry(d); p.y < 0 || p.height < 0 {
		t.Fatal("dialog before tab creation panics or has negative dimensions")
	}
}

func TestPopupResumeConfirmationNotFilterable(t *testing.T) {
	m := testModel(t, "")
	tab := m.current()
	d := m.newDialog("resume", "Resume?", "")
	d.options = []option{{Label: "Cancel", Value: "no"}, {Label: "Resume", Value: "/tmp/history.jsonl"}}
	tab.dialogs = []*dialog{d}
	press(m, 'x', 0, "x")
	if d.editor.Value() != "" || len(m.filtered(d)) != 2 {
		t.Fatal("hidden filter changed confirmation choices")
	}
}
