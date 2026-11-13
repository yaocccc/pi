package render

import (
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

const bashOutputRows = 8

func bashDetailRows(item session.Item, theme config.Theme, width int) []string {
	width = max(1, width)
	v := toolVisual{theme: theme, width: width}
	args := visualMap(item.Arguments)
	gap := func() {
		if len(v.rows) > 0 && v.rows[len(v.rows)-1] != "" {
			v.rows = append(v.rows, "")
		}
	}
	var keys []string
	for key := range args {
		if key != "command" && key != "cmd" && key != "script" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var parameters []string
	for _, key := range keys {
		parameters = append(parameters, visualLabel(key)+": "+oneLine(visualScalar(args[key])))
	}
	var commandRows []string
	if command := visualString(args, "command", "cmd", "script"); command != "" {
		commandRows = highlightedLines(command, "", "bash", theme)
	}
	if len(parameters) > 0 {
		if len(commandRows) == 0 {
			commandRows = []string{""}
		}
		last := len(commandRows) - 1
		separator := ""
		if commandRows[last] != "" {
			separator = " "
		}
		commandRows[last] += separator + paint("("+strings.Join(parameters, ", ")+")", visualDim(theme))
	}
	for _, line := range commandRows {
		v.rows = append(v.rows, visualWrap(line, width)...)
	}
	if len(item.Arguments) > 0 {
		if _, ok := visualJSON(item.Arguments); !ok {
			gap()
			v.line("Parameters unavailable · incomplete or invalid payload", theme.Warning)
		}
	}
	gap()
	output := cleanCode(item.Text)
	if strings.TrimSpace(output) == "" {
		v.line("(no output)", visualDim(theme))
	} else {
		color := theme.Text
		if item.Error {
			color = theme.Error
		}
		lines := strings.SplitN(strings.TrimSuffix(output, "\n"), "\n", bashOutputRows+1)
		used, truncated := 0, false
		for i, line := range lines {
			remaining := bashOutputRows - used
			// Bound wrapping of long single-line output as well as multiline output.
			head := ansi.Truncate(line, width*remaining+1, "")
			wrapped := visualWrap(head, width)
			take := min(remaining, len(wrapped))
			for _, row := range wrapped[:take] {
				v.rows = append(v.rows, paint(row, color))
			}
			used += take
			if take < len(wrapped) || len(head) < len(line) || (used == bashOutputRows && i < len(lines)-1) {
				truncated = true
				break
			}
		}
		if truncated {
			v.line("… (truncated)", theme.Muted)
		}
	}
	// Preserve useful saved exit/truncation metadata without a section heading.
	details := visualMap(item.Details)
	keys = keys[:0]
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		gap()
		for _, key := range keys {
			v.structured(details[key], 0, key)
		}
	}
	return v.rows
}
