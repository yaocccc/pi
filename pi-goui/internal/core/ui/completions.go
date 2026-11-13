package ui

import (
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// Category bands keep exact, prefix, substring and ordered-subsequence matches
// stable even when a command/path is substantially longer than the query.
func fuzzyScore(value, query string) (int, bool) {
	value, query = strings.ToLower(value), strings.ToLower(query)
	if query == "" {
		return 0, true
	}
	if value == query {
		return 0, true
	}
	if strings.HasPrefix(value, query) {
		return 1000000 + len([]rune(value)), true
	}
	if pos := strings.Index(value, query); pos >= 0 {
		return 2000000 + pos + len([]rune(value)), true
	}
	hay, needle := []rune(value), []rune(query)
	at, start, last := 0, -1, -1
	for i, r := range hay {
		if at < len(needle) && r == needle[at] {
			if start < 0 {
				start = i
			}
			last = i
			at++
		}
	}
	if at != len(needle) {
		return 0, false
	}
	return 3000000 + (last-start-len(needle)+1)*2 + len(hay), true
}
func slashParts(value string) (query, args string, ok bool) {
	if !strings.HasPrefix(value, "/") {
		return "", "", false
	}
	value = strings.TrimLeftFunc(value[1:], unicode.IsSpace)
	for i, r := range value {
		if unicode.IsSpace(r) {
			return value[:i], strings.TrimLeftFunc(value[i:], unicode.IsSpace), true
		}
	}
	return value, "", true
}
func (m *Model) completion(t *tab) []command {
	if m.completionHidden || t == nil {
		return nil
	}
	if mention, ok := mentionAt(t.editor.Value(), t.editor.CursorOffset()); ok {
		return m.fileChoices(mention)
	}
	query, _, ok := slashParts(t.editor.Value())
	if !ok {
		return nil
	}
	all := append(append([]command{}, localCommands...), t.commands...)
	type scored struct {
		command
		score int
	}
	var found []scored
	seen := map[string]bool{}
	for _, c := range all {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		if score, yes := fuzzyScore(c.Name, query); yes {
			found = append(found, scored{c, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score < found[j].score
		}
		return found[i].Name < found[j].Name
	})
	out := make([]command, 0, min(len(found), 64))
	for _, c := range found[:min(len(found), 64)] {
		out = append(out, c.command)
	}
	return out
}
func (m *Model) completionLabel(t *tab, c command) string {
	if c.Description == "parent directory" {
		return "../"
	}
	if _, ok := mentionAt(t.editor.Value(), t.editor.CursorOffset()); ok {
		return "@" + c.Name
	}
	return "/" + c.Name
}
func (m *Model) complete(t *tab, cs []command) {
	if len(cs) == 0 {
		return
	}
	i := min(max(0, m.completionIndex), len(cs)-1)
	if mention, ok := mentionAt(t.editor.Value(), t.editor.CursorOffset()); ok {
		m.insertFile(t, mention, cs[i])
		return
	}
	_, args, _ := slashParts(t.editor.Value())
	value := "/" + cs[i].Name + " " + args
	t.setValue(value)
	m.completionHidden = true
	m.completionIndex = 0
	m.resizeEditor(t)
}
func (m *Model) executeCompletion(t *tab, cs []command) tea.Cmd {
	if len(cs) == 0 {
		return nil
	}
	i := min(max(0, m.completionIndex), len(cs)-1)
	if mention, ok := mentionAt(t.editor.Value(), t.editor.CursorOffset()); ok {
		m.insertFile(t, mention, cs[i])
		return nil
	}
	_, args, _ := slashParts(t.editor.Value())
	value := "/" + cs[i].Name
	if args != "" {
		value += " " + args
	}
	t.setValue(value)
	m.completionHidden = true
	m.completionIndex = 0
	m.resizeEditor(t)
	return m.submit(t, false)
}
