package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
)

type detailUpdate string

type detailInspector struct {
	extension.Inspector
	text    string
	version uint64
	renders int
}

func (d *detailInspector) Matches(item session.Item, key string) bool {
	return item.ID == "write-1" && key == "detail"
}
func (d *detailInspector) Resolve(item session.Item) (session.Item, bool) { return item, true }
func (d *detailInspector) Version() uint64                                { return d.version }
func (d *detailInspector) View() extension.InspectorView {
	return extension.InspectorView{Title: "Optional detail"}
}
func (d *detailInspector) Poll(session.Item, time.Time) tea.Cmd { return nil }
func (d *detailInspector) Update(msg tea.Msg, _ *session.Item) (tea.Cmd, bool) {
	value, ok := msg.(detailUpdate)
	if ok {
		d.text = string(value)
		d.version++
	}
	return nil, ok
}
func (d *detailInspector) Rows(session.Item, config.Theme, int, func(session.State, int, bool, bool, map[string]bool) render.Options, string) []string {
	d.renders++
	return []string{d.text}
}

type detailModule struct{ detail *detailInspector }

func (m detailModule) Capabilities() extension.Capabilities {
	return extension.Capabilities{
		NewInspector: func(item session.Item, key string, _ bool, _ extension.Client, _ *render.ToolViews) (extension.Inspector, bool) {
			return m.detail, m.detail.Matches(item, key)
		},
		IsInspectorMessage: func(msg tea.Msg) bool {
			_, ok := msg.(detailUpdate)
			return ok
		},
	}
}

func TestExtensionDetailVersionRefreshesWithoutChangingToolItem(t *testing.T) {
	m := inspectorFixture(t)
	detail := &detailInspector{text: "first detail"}
	m.extensions = extension.New([]extension.Extension{detailModule{detail}})
	m.openExtensionInspector(m.current(), "write-1", "detail")
	rows := func() string { return strings.Join(m.inspectorRows(), "\n") }
	if got := rows(); !strings.Contains(got, "first detail") {
		t.Fatalf("missing initial detail: %q", got)
	}
	renders := detail.renders
	rows()
	if detail.renders != renders {
		t.Fatal("unchanged version bypassed cache")
	}
	m.Update(detailUpdate("updated detail"))
	if got := rows(); !strings.Contains(got, "updated detail") || detail.renders != renders+1 {
		t.Fatalf("provider version did not refresh detail: %q (renders=%d)", got, detail.renders)
	}
	if m.current().state.Items[0].Text != "Wrote file" {
		t.Fatal("provider changed the native tool record")
	}

	m.inspector.selection.dragging = true
	renders = detail.renders
	m.Update(detailUpdate("after selection"))
	if got := rows(); !strings.Contains(got, "updated detail") || detail.renders != renders {
		t.Fatal("provider update changed rows during selection")
	}
	m.inspector.selection.dragging = false
	if got := rows(); !strings.Contains(got, "after selection") || detail.renders != renders+1 {
		t.Fatalf("pending version did not refresh after selection: %q", got)
	}
}
