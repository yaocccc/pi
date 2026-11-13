package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

func visualJSON(data []byte) (any, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, true
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return v, true
}
func visualMap(raw json.RawMessage) map[string]any {
	v, ok := visualJSON(raw)
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}
func visualString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func visualInt(m map[string]any, key string, fallback int) int {
	v := m[key]
	switch n := v.(type) {
	case json.Number:
		i, e := strconv.Atoi(string(n))
		if e == nil && i > 0 && i < 1<<30 {
			return i
		}
	case float64:
		if n > 0 && n < 1<<30 {
			return int(n)
		}
	}
	return fallback
}
func visualName(name string) string {
	name = strings.ToLower(name)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
func visualLabel(key string) string {
	var b strings.Builder
	for i, r := range key {
		if r == '_' || r == '-' {
			b.WriteByte(' ')
			continue
		}
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte(' ')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	label := strings.TrimSpace(b.String())
	if label == "" {
		return "Field"
	}
	r := []rune(label)
	r[0] = unicode.ToUpper(r[0])
	return Clean(string(r))
}
func visualScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "—"
	case string:
		return Clean(x)
	case bool:
		if x {
			return "yes"
		}
		return "no"
	case json.Number:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

type toolVisual struct {
	theme config.Theme
	width int
	rows  []string
}

func (v *toolVisual) line(text, color string) {
	v.rows = append(v.rows, visualWrap(paint(Clean(text), color), v.width)...)
}
func (v *toolVisual) heading(s string) {
	if len(v.rows) > 0 && v.rows[len(v.rows)-1] != "" {
		v.rows = append(v.rows, "")
	}
	v.line(s, v.theme.Muted)
}
func (v *toolVisual) source(s, path, language string, start int) {
	v.rows = append(v.rows, codeRows(s, path, language, start, v.width, v.theme)...)
}
func (v *toolVisual) literal(s string) { v.rows = append(v.rows, visualWrap(cleanCode(s), v.width)...) }
func (v *toolVisual) structured(value any, depth int, label string) {
	indent := strings.Repeat(" ", min(depth*2, max(0, v.width/4)))
	switch x := value.(type) {
	case map[string]any:
		if label != "" {
			v.line(indent+visualLabel(label), v.theme.Accent)
		}
		if len(x) == 0 {
			v.line(indent+"(no fields)", visualDim(v.theme))
			return
		}
		// Image payloads are binary attachments, not useful text or source code.
		mimeType := visualString(x, "mimeType", "mime_type")
		_, hasData := x["data"]
		imagePayload := strings.HasPrefix(mimeType, "image/") && hasData
		if imagePayload {
			v.line(indent+"Image attachment · "+mimeType+" · binary data retained in history", v.theme.Accent)
		}
		keys := make([]string, 0, len(x))
		for key := range x {
			if imagePayload && (key == "data" || key == "mimeType" || key == "mime_type") {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		path := visualString(x, "path", "file_path", "filePath", "filename")
		for _, key := range keys {
			child := x[key]
			s, isString := child.(string)
			if isString && (key == "code" || key == "command" || (key == "content" && path != "")) {
				v.line(indent+visualLabel(key), v.theme.Muted)
				language := ""
				if key == "code" {
					language = "javascript"
				}
				if key == "command" {
					language = "bash"
				}
				v.source(s, path, language, 1)
				continue
			}
			v.structured(child, depth+1, key)
		}
	case []any:
		if label != "" {
			v.line(indent+fmt.Sprintf("%s · %d item(s)", visualLabel(label), len(x)), v.theme.Accent)
		}
		if len(x) == 0 {
			v.line(indent+"(empty list)", visualDim(v.theme))
		}
		if v.table(x, indent) {
			return
		}
		for i, child := range x {
			v.structured(child, depth+1, fmt.Sprintf("%d", i+1))
		}
	case string:
		if nested, ok := visualJSON([]byte(x)); ok {
			switch nested.(type) {
			case map[string]any, []any:
				v.structured(nested, depth, label)
				return
			}
		}
		prefix := ""
		if label != "" {
			prefix = visualLabel(label) + ": "
		}
		if strings.Contains(x, "\n") {
			if label != "" {
				v.line(indent+visualLabel(label), v.theme.Muted)
			}
			for _, line := range strings.Split(cleanCode(x), "\n") {
				v.line(indent+line, v.theme.Text)
			}
		} else {
			v.line(indent+prefix+x, v.theme.Text)
		}
	default:
		prefix := ""
		if label != "" {
			prefix = visualLabel(label) + ": "
		}
		v.line(indent+prefix+visualScalar(x), v.theme.Text)
	}
}
func (v *toolVisual) arguments(raw json.RawMessage, skip ...string) {
	value, ok := visualJSON(raw)
	if !ok {
		v.heading("Parameters unavailable · incomplete or invalid payload")
		return
	}
	if value == nil {
		return
	}
	if m, ok := value.(map[string]any); ok {
		copyMap := map[string]any{}
		for k, val := range m {
			copyMap[k] = val
		}
		for _, k := range skip {
			delete(copyMap, k)
		}
		if len(copyMap) == 0 {
			return
		}
		value = copyMap
	}
	v.heading("Parameters")
	v.structured(value, 0, "")
}
func (v *toolVisual) result(item session.Item) {
	title := "Result"
	if item.Error {
		title = "Error · completion not confirmed"
	} else if item.Running {
		title = "Partial result"
	}
	v.heading(title)
	if item.Text != "" {
		if value, ok := visualJSON([]byte(item.Text)); ok {
			v.structured(value, 0, "")
		} else {
			v.literal(item.Text)
		}
	} else if item.Running {
		v.line("Waiting for output…", v.theme.Warning)
	} else if item.HasResult {
		v.line("(empty result)", visualDim(v.theme))
	} else {
		v.line("No result recorded", visualDim(v.theme))
	}
}
func (v *toolVisual) metadata(raw json.RawMessage, skip ...string) {
	value, ok := visualJSON(raw)
	if !ok {
		v.heading("Result metadata unavailable · invalid payload")
		return
	}
	if value == nil {
		return
	}
	if m, ok := value.(map[string]any); ok {
		copyMap := map[string]any{}
		for k, val := range m {
			copyMap[k] = val
		}
		for _, k := range skip {
			delete(copyMap, k)
		}
		if len(copyMap) == 0 {
			return
		}
		if truncation, ok := copyMap["truncation"].(map[string]any); ok {
			info := map[string]any{}
			for key, field := range truncation {
				if key != "content" {
					info[key] = field
				}
			}
			copyMap["truncation"] = info
		}
		value = copyMap
	}
	v.heading("Metadata")
	v.structured(value, 0, "")
}

// toolDetailRows renders saved payloads only. No preview reads or executes files.
// The caller owns the invocation row, frame, folding and transcript offsets.
func toolDetailRows(item session.Item, theme config.Theme, width int, providers ...*ToolViews) []string {
	if width <= 0 {
		return nil
	}
	tools := firstTools(providers)
	if tools != nil && tools.Detail != nil {
		if rows, ok := tools.Detail(cardHost(), item, theme, width); ok {
			return rows
		}
	}
	if visualName(item.ToolName) == "read" {
		return readDetailRows(item, theme, width)
	}
	if visualName(item.ToolName) == "bash" {
		return bashDetailRows(item, theme, width)
	}
	if visualName(item.ToolName) == "codemode" {
		return codemodeRows(item, theme, width)
	}
	if name := visualName(item.ToolName); name == "edit" || name == "write" {
		return changeDiffRows(item, theme, width)
	}
	if genericTool(item.ToolName, tools) {
		return genericToolRows(item, theme, width)
	}
	v := &toolVisual{theme: theme, width: width}
	args := visualMap(item.Arguments)
	name := visualName(item.ToolName)
	switch name {
	case "bash", "shell", "powershell":
		command := visualString(args, "command", "cmd", "script")
		if command != "" {
			v.heading("Command")
			language := "bash"
			if name == "powershell" {
				language = "powershell"
			}
			v.source(command, "", language, 1)
		}
		v.arguments(item.Arguments, "command", "cmd", "script")
		v.result(item)
	}
	v.metadata(item.Details)
	if len(v.rows) == 0 {
		v.line("No displayable result", visualDim(theme))
	}
	return v.rows
}
func sourceLineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}
func visualEdits(args map[string]any) []map[string]any {
	var out []map[string]any
	value := args["edits"]
	if s, ok := value.(string); ok {
		if decoded, valid := visualJSON([]byte(s)); valid {
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
func readSource(text string, details map[string]any) (source, note string) {
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

// toolPreview is a plain, compact description. It never includes object JSON;
// its caller is responsible for clipping and per-role coloring.
func toolPreview(item session.Item) string {
	args := visualMap(item.Arguments)
	details := visualMap(item.Details)
	target := visualString(args, "path", "file_path", "filePath", "filename", "command", "cmd", "query", "pattern", "url", "code")
	name := visualName(item.ToolName)
	var summary string
	if name == "edit" {
		if diff := visualString(details, "diff", "patch"); diff != "" {
			a, d := diffCounts(diff)
			summary = fmt.Sprintf("+%d / −%d", a, d)
		} else if edits := visualEdits(args); len(edits) > 0 {
			summary = fmt.Sprintf("%d replacement(s)", len(edits))
		}
	}
	if name == "write" {
		if content, ok := args["content"].(string); ok {
			summary = fmt.Sprintf("%d lines · %d bytes", sourceLineCount(content), len(content))
		}
	}
	if name == "read" {
		if offset := visualInt(args, "offset", 0); offset > 0 {
			summary = fmt.Sprintf("from line %d", offset)
		}
	}
	if summary == "" && item.Text != "" {
		if obj, ok := visualJSON([]byte(item.Text)); ok {
			summary = resultSynopsis(obj)
		} else {
			lines := strings.Split(strings.TrimSpace(Clean(item.Text)), "\n")
			summary = lines[0]
			if len(lines) > 1 {
				summary += fmt.Sprintf(" · %d lines", len(lines))
			}
		}
	}
	if item.Error {
		// A failed write/edit must preview its error, not the requested change.
		if strings.TrimSpace(Clean(item.Text)) == "" {
			summary = "Failed · no error output"
		} else if value, ok := visualJSON([]byte(item.Text)); ok {
			summary = resultSynopsis(value)
		} else {
			summary = strings.Split(strings.TrimSpace(Clean(item.Text)), "\n")[0]
		}
	}
	if summary == "" {
		switch {
		case item.Error:
			summary = "Failed · no error output"
		case item.Running:
			summary = "Waiting for output…"
		case item.HasResult:
			summary = "Empty result"
		}
	}
	if target == "" && summary == "" {
		if tasks, ok := args["tasks"].([]any); ok {
			summary = fmt.Sprintf("%d task(s)", len(tasks))
		} else {
			target = visualString(args, "question", "objective", "prompt")
		}
	}
	target, summary = oneLine(target), oneLine(summary)
	if target != "" && summary != "" {
		return target + " · " + summary
	}
	return target + summary
}
