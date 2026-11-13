package detail_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/render/tooldetail"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender"
	"pi-goui/internal/extensions/toolrender/detail"
	"strings"
	"testing"
)

func testHooks(t *testing.T) tooldetail.Hooks {
	t.Helper()
	return tooldetail.Hooks{
		Line: func(text, color string, width int) []string {
			if width < 1 {
				t.Fatal("nonpositive rendering width")
			}
			return strings.Split(text, "\n")
		},
		Clean: func(s string) string { return s },
		CodeRows: func(source, path, language string, start, width int, theme config.Theme) []string {
			return []string{fmt.Sprintf("code:%s:%s:%d", path, language, start), source}
		},
		DiffRows:   func(diff string, width int, theme config.Theme) []string { return []string{diff} },
		DiffCounts: func(string) (int, int) { return 1, 1 },
	}
}

func decodeMap(raw []byte) map[string]any {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	_ = decoder.Decode(&value)
	return value
}
func copyMap(src map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range src {
		out[k] = v
	}
	return out
}

// probe exercises consumption of caller-provided decoded maps separately from
// rendering. No formatting is imitated: output assertions use the real host.
func probe(width int, hooks tooldetail.Hooks) *detail.Renderer {
	rows := []string{}
	return detail.New(extensionapi.DetailHost{Width: width, Hooks: hooks, Rows: &rows,
		Line: func(string, string) {}, Heading: func(string) {}, Output: func(string) {},
		Value: func(any, string) {}, Field: func(m map[string]any, k, label string) { delete(m, k) },
		Scalar: func(v any) string { return fmt.Sprint(v) },
	})
}
func renderAdapter(t *testing.T, name string, item session.Item, args, details map[string]any, width int, hooks tooldetail.Hooks) []string {
	t.Helper()
	var err error
	item.ToolName = name
	if item.Arguments, err = json.Marshal(args); err != nil {
		t.Fatal(err)
	}
	if item.Details, err = json.Marshal(details); err != nil {
		t.Fatal(err)
	}
	if !probe(width, hooks).Render(name, item, args, details) {
		t.Fatalf("adapter did not handle %q", name)
	}
	return tooldetail.Render(item, config.Theme{}, width, hooks, toolrender.NewViews().FullDetail)
}
