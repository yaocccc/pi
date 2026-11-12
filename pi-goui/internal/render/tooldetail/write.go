package tooldetail

import (
	"fmt"
	"strings"

	"pi-goui/internal/session"
)

func (v *renderer) write(item session.Item, args, details map[string]any) {
	path := stringField(args, "path", "file_path", "filePath", "filename")
	state, marker, color := "writing", "◌", v.theme.Warning
	if item.HasResult && !item.Running {
		state, marker, color = "written", "✓", v.theme.Success
	}
	v.line(marker+" "+state+" "+path, color)
	if content, ok := args["content"].(string); ok {
		lines := 0
		if content != "" {
			lines = strings.Count(strings.TrimSuffix(content, "\n"), "\n") + 1
		}
		qualifier := ""
		if state == "writing" {
			qualifier = " · preview"
		}
		v.line(fmt.Sprintf("Content · %d lines · %d B%s", lines, len(content), qualifier), v.theme.Muted)
		v.line(strings.Repeat("─", v.width), v.theme.Muted)
		v.writeGrid(content, path, color)
	} else {
		v.line("(waiting for content)", v.theme.Muted)
	}
	v.output(item.Text)
}

// Reuse the transcript write grid's colored change rail and numbered syntax
// gutter, but wrap every source line instead of truncating to its last ten lines.
// The preview rail stays amber until the tool actually confirms the write.
func (v *renderer) writeGrid(content, path, color string) {
	if v.width < 10 || content == "" {
		v.code(content, path, "", 1)
		return
	}
	rail := strings.Join(v.hooks.Line("▌ ", color, 2), "")
	for _, row := range v.hooks.CodeRows(content, path, "", 1, v.width-2, v.theme) {
		v.rows = append(v.rows, rail+row)
	}
}
