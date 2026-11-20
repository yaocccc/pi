package tooldetail

import (
	"fmt"
	"strings"

	"pi-goui/internal/core/render/tooldata"
	"pi-goui/internal/core/session"
)

func (v *renderer) read(item session.Item, args, details map[string]any) {
	path := stringField(args, "path", "file_path", "filePath", "filename")
	start := max(1, intField(args, "offset", 1))
	v.line(fmt.Sprintf("File · %s · from line %d", path, start), v.theme.Muted)
	if strings.TrimSpace(item.Text) == "[image]" || strings.HasPrefix(item.Text, "Read image file") {
		// Match the transcript's attachment presentation, not a numbered code file.
		v.heading("Image attachment")
		v.line(item.Text, v.theme.Text)
		return
	}
	if item.Text != "" {
		source, note := tooldata.ReadSource(item.Text, details)
		if source != "" {
			lines := strings.Count(strings.TrimSuffix(source, "\n"), "\n") + 1
			// Keep the transcript's quiet file header and numbered source gutter;
			// a single compact caption separates metadata from the full document.
			v.line(fmt.Sprintf("Content · Lines %d–%d · %d B received", start, start+lines-1, len(source)), v.theme.Muted)
			v.line(strings.Repeat("─", v.width), v.theme.Muted)
			v.code(source, path, "", start)
		}
		if note != "" {
			v.heading("Read notice")
			v.line(note, v.theme.Warning)
		}
		// The read API repeats the visible source inside truncation.
		if trunc, ok := details["truncation"].(map[string]any); ok {
			trunc = copyMap(trunc)
			if saved, ok := trunc["content"].(string); ok && saved == source {
				delete(trunc, "content")
			}
			if len(trunc) > 0 {
				details["truncation"] = trunc
			} else {
				delete(details, "truncation")
			}
		}
	} else if len(item.Images) == 0 {
		if item.HasResult && !item.Running {
			v.line("(empty result)", v.theme.Muted)
		} else {
			v.line("(waiting for result)", v.theme.Muted)
		}
	}
}
