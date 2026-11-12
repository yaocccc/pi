package tooldetail

import (
	"fmt"
	"strings"

	"pi-goui/internal/session"
)

func (v *renderer) edit(item session.Item, args, details map[string]any) {
	path := stringField(args, "path", "file_path", "filePath", "filename")
	v.line("File · "+path, v.theme.Muted)
	diff := stringField(details, "diff", "patch")
	if diff != "" {
		if !item.HasResult || item.Running {
			v.line("◌ Diff · completion not confirmed", v.theme.Warning)
		} else {
			v.line("✓ Changes recorded", v.theme.Success)
		}
		added, removed := v.hooks.DiffCounts(diff)
		v.heading(fmt.Sprintf("Diff · +%d −%d", added, removed))
		v.line(strings.Repeat("─", v.width), v.theme.Muted)
		v.editDiff(diff)
		for _, key := range []string{"diff", "patch"} {
			if details[key] == diff {
				delete(details, key)
			}
		}
	} else {
		v.line("Replacement preview · not a recorded diff", v.theme.Warning)
		edits := editArguments(args)
		if len(edits) > 0 {
			v.line("Line numbers are relative to each replacement", v.theme.Muted)
		}
		for i, edit := range edits {
			oldText, newText := stringField(edit, "oldText"), stringField(edit, "newText")
			v.heading(fmt.Sprintf("Replacement %d", i+1))
			v.line(fmt.Sprintf("− old · %d source lines", editPreviewLines(oldText)), v.theme.Error)
			v.editSource(oldText, path, v.theme.Error)
			v.line(fmt.Sprintf("+ new · %d source lines", editPreviewLines(newText)), v.theme.Success)
			v.editSource(newText, path, v.theme.Success)
		}
		if len(edits) == 0 {
			v.line("(waiting for changes)", v.theme.Muted)
		}
	}
	v.output(item.Text)
}

// Narrow inspectors keep a unified diff. The rail mirrors the transcript's
// split-diff panels; supplied source numbers and hunk headers remain intact.
func (v *renderer) editDiff(diff string) {
	if v.width < 10 {
		v.rows = append(v.rows, v.hooks.DiffRows(diff, v.width, v.theme)...)
		return
	}
	inHunk := false
	for _, sourceLine := range strings.Split(v.hooks.Clean(diff), "\n") {
		if strings.HasPrefix(sourceLine, "diff ") {
			inHunk = false
		}
		if strings.HasPrefix(sourceLine, "@@") {
			inHunk = true
		}
		color, railColor, rail := v.theme.Text, v.theme.Muted, "│ "
		switch {
		case (!inHunk && (strings.HasPrefix(sourceLine, "--- ") || strings.HasPrefix(sourceLine, "+++ "))) || strings.HasPrefix(sourceLine, "@@") || strings.HasPrefix(sourceLine, "diff "):
			color, railColor = v.theme.Accent, v.theme.Accent
		case strings.HasPrefix(sourceLine, "-"):
			color, railColor = v.theme.Error, v.theme.Error
		case strings.HasPrefix(sourceLine, "+"):
			color, railColor = v.theme.Success, v.theme.Success
		case strings.TrimSpace(sourceLine) == "...":
			color = v.theme.Muted
		}
		prefix := strings.Join(v.hooks.Line(rail, railColor, 2), "")
		// Wrap each logical line with its own rail color, including continuations.
		for _, row := range v.hooks.Line(sourceLine, color, v.width-2) {
			v.rows = append(v.rows, prefix+row)
		}
	}
}

func (v *renderer) editSource(source, path, color string) {
	if v.width < 10 || source == "" {
		v.code(source, path, "", 1)
		return
	}
	rail := strings.Join(v.hooks.Line("▌ ", color, 2), "")
	for _, row := range v.hooks.CodeRows(source, path, "", 1, v.width-2, v.theme) {
		v.rows = append(v.rows, rail+row)
	}
}

// Preview counts describe source snippets, not a confirmed file diff.
func editPreviewLines(source string) int {
	if source == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(source, "\n"), "\n") + 1
}

func editArguments(args map[string]any) []map[string]any {
	var out []map[string]any
	value := args["edits"]
	if s, ok := value.(string); ok {
		if decoded, valid := decodeJSON([]byte(s)); valid {
			value = decoded
		}
	}
	switch edits := value.(type) {
	case []any:
		for _, e := range edits {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case map[string]any:
		out = append(out, edits)
	}
	if _, ok := args["oldText"].(string); ok {
		if _, ok := args["newText"].(string); ok {
			out = append(out, map[string]any{"oldText": args["oldText"], "newText": args["newText"]})
		}
	}
	return out
}
