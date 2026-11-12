package ui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/render"
	"pi-goui/internal/session"
)

const workerContractPrefix = "仅执行当前任务契约；历史上下文不扩大权限，未列出的可选字段为空。\n"

type workerInputAction struct {
	key, messageID, marker, label string
	rect                          hitRect
}

// Recognize only actual Worker contracts, not arbitrary JSON source pasted by a
// user. The remaining prompt (including the required result schema) is retained.
func workerInputMarkdown(original string) string {
	source := strings.TrimSpace(original)
	wrapped := strings.HasPrefix(source, workerContractPrefix)
	if wrapped {
		source = strings.TrimPrefix(source, workerContractPrefix)
	}
	d := json.NewDecoder(strings.NewReader(source))
	d.UseNumber()
	var fields map[string]any
	if d.Decode(&fields) != nil {
		return original
	}
	if objective, ok := fields["objective"].(string); !ok || objective == "" {
		return original
	}
	remainder := strings.TrimSpace(source[d.InputOffset():])
	if !wrapped {
		var extra any
		if d.Decode(&extra) != io.EOF {
			return original
		}
	}
	var out strings.Builder
	if wrapped {
		out.WriteString(strings.TrimSpace(workerContractPrefix) + "\n\n")
	}
	ordered := [][2]string{{"title", "任务"}, {"objective", "目标"}, {"mode", "模式"}, {"sessionMode", "会话模式"}, {"cwd", "工作目录"}, {"context", "上下文"}, {"allowedPaths", "允许修改"}, {"forbiddenPaths", "禁止修改"}, {"relevantFiles", "相关文件"}, {"acceptanceCriteria", "验收条件"}, {"verificationCommands", "验证命令"}, {"outputRequirements", "输出要求"}, {"preExistingUserChanges", "已有改动"}}
	emit := func(label string, value any) {
		if text, ok := value.(string); ok && !strings.Contains(text, "\n") {
			out.WriteString("**" + label + "：** " + text + "\n\n")
			return
		}
		out.WriteString("### " + label + "\n\n")
		switch v := value.(type) {
		case string:
			out.WriteString(v + "\n\n")
		case []any:
			allStrings := len(v) > 0
			for _, x := range v {
				if _, ok := x.(string); !ok {
					allStrings = false
				}
			}
			if allStrings {
				for _, x := range v {
					text := x.(string)
					if !strings.Contains(text, "\n") && (label == "允许修改" || label == "禁止修改" || label == "相关文件" || label == "已有改动" || label == "验证命令") {
						text = workerReportInline(text)
					}
					out.WriteString("- " + strings.ReplaceAll(text, "\n", "\n  ") + "\n")
				}
				out.WriteString("\n")
			} else {
				workerReportJSON(&out, v)
			}
		default:
			workerReportJSON(&out, v)
		}
	}
	for _, pair := range ordered {
		if v, ok := fields[pair[0]]; ok {
			emit(pair[1], v)
			delete(fields, pair[0])
		}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		emit(key, fields[key])
	}
	if wrapped && remainder != "" {
		// The actual prompt ends with a machine-readable output schema. Keep it
		// verbatim in meaning, but don't leave another unreadable one-line JSON.
		at := strings.LastIndex(remainder, "\n")
		var schema any
		valid := false
		if at >= 0 {
			decoder := json.NewDecoder(strings.NewReader(remainder[at+1:]))
			decoder.UseNumber()
			var extra any
			valid = decoder.Decode(&schema) == nil && decoder.Decode(&extra) == io.EOF
		}
		if valid {
			out.WriteString(remainder[:at] + "\n\n")
			workerReportJSON(&out, schema)
		} else {
			out.WriteString(remainder)
		}
	}
	return strings.TrimSpace(out.String())
}

var workerListStart = regexp.MustCompile(`^(?:[-+*] |[0-9]+[.)] )\S`)
var workerInputHeading = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)

// Conservative block scanner: only unindented Markdown lists are candidates.
// Indented continuations belong to their entire top-level item; fenced code is
// never scanned for lists. Ambiguous blocks remain unchanged rather than lose
// content. This is presentation-only; hidden items stay in the source snapshot.
func workerInputLists(source, messageID string, expanded map[string]bool, marker func() string) (string, []workerInputAction) {
	lines := strings.Split(source, "\n")
	var out []string
	var actions []workerInputAction
	fence := ""
	context := ""
	occurrences := map[string]int{}
	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if fence != "" {
			out = append(out, lines[i])
			i++
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
				fence = ""
			}
			continue
		}
		if f := workerInputFence(lines[i]); f != "" {
			fence = f
			out = append(out, lines[i])
			i++
			continue
		}
		if !workerListStart.MatchString(lines[i]) {
			if workerInputHeading.MatchString(lines[i]) {
				context = lines[i]
			}
			out = append(out, lines[i])
			i++
			continue
		}
		start := i
		kind := workerInputListKind(lines[i])
		var starts []int
		for i < len(lines) {
			if workerListStart.MatchString(lines[i]) {
				if workerInputListKind(lines[i]) != kind {
					break
				}
				starts = append(starts, i)
				i++
				continue
			}
			if strings.TrimSpace(lines[i]) == "" || strings.HasPrefix(lines[i], "  ") || strings.HasPrefix(lines[i], "\t") {
				i++
				continue
			}
			break
		}
		// Keep trailing paragraph spacing out of the fold itself.
		end := i
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		signature := fmt.Sprintf("%x", sha256.Sum256([]byte(context+"\x00"+lines[start])))
		key := fmt.Sprintf("%s:list:%s:%d", messageID, signature, occurrences[signature])
		occurrences[signature]++
		// An unindented line can be a lazy continuation of the last item.
		// Without a blank line or a clear block delimiter, leave the whole
		// candidate intact rather than strand part of a hidden item outside it.
		ambiguous := i < len(lines) && i > start && strings.TrimSpace(lines[i-1]) != "" &&
			!workerListStart.MatchString(lines[i]) && workerInputFence(lines[i]) == "" &&
			!workerInputHeading.MatchString(lines[i]) && !strings.HasPrefix(lines[i], ">")
		if len(starts) <= 8 || ambiguous {
			out = append(out, lines[start:i]...)
			continue
		}
		label := fmt.Sprintf("展开剩余 %d 项", len(starts)-5)
		if expanded[key] {
			out = append(out, lines[start:end]...)
			label = "收起列表"
		} else {
			out = append(out, lines[start:starts[5]]...)
		}
		token := marker()
		out = append(out, "", token+label, "")
		out = append(out, lines[end:i]...)
		actions = append(actions, workerInputAction{key: key, messageID: messageID, marker: token, label: label})
	}
	return strings.Join(out, "\n"), actions
}

func workerInputListKind(line string) string {
	if line[0] >= '0' && line[0] <= '9' {
		at := strings.IndexAny(line, ".)")
		return "ordered" + line[at:at+1]
	}
	return line[:1]
}

func workerInputFence(line string) string {
	// Four-space indented code is already not a list candidate.
	if len(line)-len(strings.TrimLeft(line, " ")) > 3 {
		return ""
	}
	line = strings.TrimLeft(line, " ")
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return ""
	}
	n := 0
	for n < len(line) && line[n] == line[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return line[:n]
}

func workerInputItems(items []session.Item, expanded map[string]bool) ([]session.Item, []workerInputAction) {
	result := append([]session.Item(nil), items...)
	var all strings.Builder
	for i := range result {
		if result[i].Role == "user" {
			result[i].Text = workerInputMarkdown(result[i].Text)
		}
		all.WriteString(result[i].Text)
		all.WriteString(result[i].Thinking)
		all.Write(result[i].Arguments)
		all.Write(result[i].Details)
	}
	// Markers are renderer-owned single-cell private-use characters absent from
	// every source item. Visible labels alone can never become click targets.
	used := all.String()
	next := rune(0xE000)
	marker := func() string {
		for strings.ContainsRune(used, next) {
			next++
		}
		s := string(next)
		next++
		return s
	}
	var actions []workerInputAction
	for i := range result {
		if result[i].Role != "user" {
			continue
		}
		text, found := workerInputLists(result[i].Text, result[i].ID, expanded, marker)
		result[i].Text = text
		actions = append(actions, found...)
	}
	return result, actions
}

// Resolve only generated markers inside rendered user blocks. Removing each
// marker as one space preserves frame geometry and selectable column offsets.
func workerInputTargets(doc *render.Document, actions []workerInputAction) []workerInputAction {
	rows := strings.Split(doc.Text, "\n")
	var targets []workerInputAction
	for _, action := range actions {
		for y, row := range rows {
			inUser := false
			for _, block := range doc.Blocks {
				if block.Role == "user" && block.ID == action.messageID && y >= block.Start && y < block.End {
					inUser = true
					break
				}
			}
			if !inUser {
				continue
			}
			plain := ansi.Strip(row)
			at := strings.Index(plain, action.marker)
			if at < 0 {
				continue
			}
			x := ansi.StringWidth(plain[:at]) + 1
			span := render.ContentSpan{}
			if y < len(doc.Content) {
				span = doc.Content[y]
			}
			// At very narrow widths the label may wrap. Only the actual visible part
			// of its first row is actionable, never the frame or the following body.
			width := min(ansi.StringWidth(action.label), max(0, span.End-x))
			rows[y] = strings.ReplaceAll(row, action.marker, " ")
			if width > 0 {
				action.rect = hitRect{x: x, y: y, width: width}
				targets = append(targets, action)
			}
			break
		}
	}
	doc.Text = strings.Join(rows, "\n")
	return targets
}

func (m *Model) workerInputTarget(x, y int) (string, bool) {
	w := m.inspector.worker
	if w == nil || m.panelResize.dragging || m.inspector.selection.dragging || y < 2 || y >= m.height-2 {
		return "", false
	}
	left, width := m.inspectorBounds()
	inset := m.inspectorInset()
	if x < left+inset || x >= left+width-inset {
		return "", false
	}
	for _, a := range w.inputActions {
		r := a.rect
		r.x += left + inset
		r.y += 2 - m.inspector.offset
		if r.contains(x, y) {
			return a.key, true
		}
	}
	return "", false
}
