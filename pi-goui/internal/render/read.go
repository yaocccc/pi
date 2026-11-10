package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

const readPreviewRows = 8

func readDetailRows(item session.Item, theme config.Theme, width int) []string {
	args := visualMap(item.Arguments)
	path := visualString(args, "path", "file_path", "filePath", "filename")
	header := oneLine(path)
	var params []string
	for _, key := range []string{"offset", "limit"} {
		if value, ok := args[key]; ok && value != nil {
			params = append(params, visualLabel(key)+": "+oneLine(visualScalar(value)))
		}
	}
	if len(params) > 0 {
		header = strings.TrimSpace(header + " (" + strings.Join(params, ", ") + ")")
	}
	var rows []string
	if header != "" {
		rows = append(rows, fit(paint(header, visualDim(theme)), width), "")
	}
	var content []string
	truncated := false
	source, note := readSource(item.Text, visualMap(item.Details))
	switch {
	case item.Error:
		content = visualWrap(paint("Error: "+Clean(item.Text), theme.Error), width)
	case strings.TrimSpace(item.Text) == "[image]" || strings.HasPrefix(item.Text, "Read image file"):
		content = visualWrap(paint("Image attachment · "+oneLine(item.Text), theme.Muted), width)
	case item.Text == "":
		text := "(no output)"
		if item.Running {
			text = "Waiting for output…"
		}
		content = visualWrap(paint(text, theme.Muted), width)
	default:
		lines := strings.SplitN(strings.TrimSuffix(source, "\n"), "\n", readPreviewRows+1)
		if len(lines) > readPreviewRows {
			lines = lines[:readPreviewRows]
			truncated = true
		}
		for i, line := range lines {
			head := ansi.Truncate(cleanCode(line), width*readPreviewRows+1, "")
			if head != cleanCode(line) {
				truncated = true
			}
			lines[i] = head
		}
		content = codeRows(strings.Join(lines, "\n"), path, "", visualInt(args, "offset", 1), width, theme)
	}
	if len(content) > readPreviewRows {
		content = content[:readPreviewRows]
		truncated = true
	}
	rows = append(rows, content...)
	if truncated {
		rows = append(rows, fit(paint("… (truncated)", theme.Muted), width))
	}
	if note != "" && !item.Error {
		rows = append(rows, fit(paint(oneLine(note), theme.Muted), width))
	}
	return rows
}
