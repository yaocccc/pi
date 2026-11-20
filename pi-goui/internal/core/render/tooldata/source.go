package tooldata

import "strings"

func SourceLineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}
func EditArguments(args map[string]any) []map[string]any {
	var out []map[string]any
	value := args["edits"]
	if s, ok := value.(string); ok {
		if decoded, valid := DecodeJSON([]byte(s)); valid {
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
func ReadSource(text string, details map[string]any) (source, note string) {
	if trunc, ok := details["truncation"].(map[string]any); ok {
		if content, ok := trunc["content"].(string); ok {
			if strings.HasPrefix(text, content) {
				return content, strings.TrimSpace(strings.TrimPrefix(text, content))
			}
		}
		if first, _ := trunc["firstLineExceedsLimit"].(bool); first {
			return "", text
		}
	}
	// Only remove pi's recognizable final continuation notices, not arbitrary
	// bracketed file content or every line resembling an informational message.
	if i := strings.LastIndex(text, "\n\n["); i >= 0 {
		tail := text[i+2:]
		if strings.HasSuffix(tail, "]") && !strings.Contains(tail, "\n") && strings.Contains(tail, "Use offset=") && (strings.HasPrefix(tail, "[Showing lines ") || strings.Contains(tail, " more lines in file.")) {
			return text[:i], tail
		}
	}
	return text, ""
}
