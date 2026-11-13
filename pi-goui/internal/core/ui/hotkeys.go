package ui

import "strings"

// Key reuse is intentional across contexts. Keep selector/search bindings out
// of the main-input group and state precedence instead of claiming exclusivity.
func (m *Model) hotkeysText() string {
	groups := []struct {
		title string
		lines []string
	}{
		{title: "Main input / session"},
		{title: "Transcript navigation"},
		{title: "Transcript search (while searching)"},
		{title: "Session selector (/resume)"},
		{title: "Menus / completions"},
		{title: "Text editing (input and editable dialogs)"},
	}
	notes := map[string]string{
		"app.clear":             "clear draft; twice on empty input exits",
		"app.exit":              "close session only when input is empty; last session exits",
		"app.interrupt":         "stop active work; otherwise dismiss completion; twice opens tree/fork",
		"app.session.delete":    "delete selected session with confirmation",
		"app.session.new":       "new session",
		"app.session.previous":  "previous open session",
		"app.session.next":      "next open session",
		"tui.input.copy":        "copy selected transcript text (only with a selection)",
		"tui.input.submit":      "send; accepts a visible completion first",
		"tui.input.tab":         "complete command or file",
		"tui.editor.cursorUp":   "previous draft at the top of main input",
		"tui.editor.cursorDown": "next draft while browsing input history",
		"tui.select.confirm":    "confirm menu choice; Enter uses submit binding for completions",
		"tui.select.pageUp":     "menus only",
		"tui.select.pageDown":   "menus only",
	}
	for _, action := range m.cfg.Keys.Actions() {
		keys := m.cfg.Keys.Keys(action)
		if len(keys) == 0 {
			continue
		}
		group := 0
		switch {
		case strings.HasPrefix(action, "tui.editor.") || action == "tui.input.newLine":
			group = 5
		case strings.HasPrefix(action, "tui.select."):
			group = 4
		case action == "app.session.toggleNamedFilter" || action == "app.session.togglePath" || action == "app.session.toggleSort" || action == "app.session.rename" || action == "app.session.delete":
			group = 3
		case action == "tui.altScreen.searchNext" || action == "tui.altScreen.searchPrevious" || action == "tui.altScreen.searchClose":
			group = 2
		case strings.HasPrefix(action, "tui.altScreen."):
			group = 1
		}
		line := "  " + strings.Join(keys, ", ") + "  " + action
		if note := notes[action]; note != "" {
			line += " — " + note
		}
		groups[group].lines = append(groups[group].lines, line)
	}
	lines := []string{
		"Bindings depend on context; the same key may have different actions.",
		"Dialogs and completions handle their own keys first.",
		"In main input, app/navigation bindings override editor bindings.",
		"Selected transcript text takes priority over clearing the draft.",
	}
	for _, group := range groups {
		if len(group.lines) == 0 {
			continue
		}
		lines = append(lines, "", group.title)
		lines = append(lines, group.lines...)
	}
	return strings.Join(lines, "\n")
}
