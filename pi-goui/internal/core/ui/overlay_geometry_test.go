package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestToolInspectorOverlayWideBackdrop(t *testing.T) {
	for _, width := range []int{60, 80, 160} {
		for _, glyph := range []string{"中", "🙂", "👩‍💻"} {
			for _, prefix := range []string{"", "a"} {
				t.Run(fmt.Sprintf("%d/%s/%s", width, glyph, prefix), func(t *testing.T) {
					m := inspectorFixture(t)
					m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
					tab := m.current()
					args, _ := json.Marshal(map[string]string{"path": prefix + strings.Repeat(glyph, 80) + ".go", "content": "中文代码 👩‍💻"})
					tab.state.Items[0].Arguments = args
					m.document(tab)
					r := inspectorTarget(t, m)
					original := tab.doc.Text
					rows := strings.Split(original, "\n")
					before := rows[r.y]
					m.sidebarHistory.pointerKnown = true
					m.sidebarHistory.pointerX, m.sidebarHistory.pointerY = r.x, r.y
					m.overlayToolInspect(rows)
					got := rows[r.y]
					if ansi.StringWidth(got) != ansi.StringWidth(before) {
						t.Fatalf("overlay changed width: got %d want %d", ansi.StringWidth(got), ansi.StringWidth(before))
					}
					if badge := ansi.Strip(ansi.Cut(got, r.x, r.x+r.width)); badge != toolInspectButton {
						t.Fatalf("button not at click rectangle: %q", badge)
					}
					if ansi.Strip(ansi.Cut(got, r.x+r.width, ansi.StringWidth(got))) != ansi.Strip(popupBackdropSlice(before, r.x+r.width, ansi.StringWidth(before))) {
						t.Fatal("right border/backdrop moved")
					}
					if tab.doc.Text != original {
						t.Fatal("overlay changed underlying content")
					}
					m.mouse(tea.Mouse{Button: tea.MouseLeft, X: r.x, Y: r.y})
					if !m.inspectorOpen() || !tab.folds["write-1"] {
						t.Fatal("button click did not match painted geometry")
					}
				})
			}
		}
	}
}

func TestToastOverlayWideBackdrop(t *testing.T) {
	for _, width := range []int{30, 60, 132} {
		for _, glyph := range []string{"中", "🙂", "👩‍💻"} {
			t.Run(fmt.Sprintf("%d/%s", width, glyph), func(t *testing.T) {
				m := testModel(t, "")
				m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				tab := m.current()
				now := time.Now()
				tab.notifyAt("保存完成\n👩‍💻", now)
				box, ok := m.toastBox(tab, now)
				if !ok || box.x < 1 || box.height != 4 {
					t.Fatal("missing toast geometry")
				}
				// Both overlay edges bisect a colored double-width grapheme.
				backdrop := strings.Repeat("p", box.x-1) + glyph + strings.Repeat("q", box.width-2) + glyph
				backdrop = m.paint(backdrop, m.cfg.Theme.Text)
				view := strings.Repeat(backdrop+"\n", m.height-1) + backdrop
				rows := strings.Split(m.overlayToast(tab, view, now), "\n")
				for i, want := range box.rows {
					got := rows[box.y+i]
					if ansi.StringWidth(got) != m.mainWidth() {
						t.Fatalf("toast changed width: got %d want %d", ansi.StringWidth(got), m.mainWidth())
					}
					if cut := ansi.Strip(ansi.Cut(got, box.x, box.x+box.width)); cut != ansi.Strip(want) {
						t.Fatalf("toast not at hit rectangle: %q want %q", cut, ansi.Strip(want))
					}
					if plain := ansi.Strip(got); plain != strings.Repeat("p", box.x-1)+" "+ansi.Strip(want)+" " {
						t.Fatalf("uncovered text moved: %q", plain)
					}
				}
				if rows[box.height] != backdrop {
					t.Fatal("toast changed unrelated row")
				}
				m.mouse(tea.Mouse{Button: tea.MouseLeft, X: box.x, Y: box.y + box.height - 1})
				if tab.toast.text != "" || tab.selection.dragging {
					t.Fatal("visible toast click missed or reached backdrop")
				}
			})
		}
	}
}
