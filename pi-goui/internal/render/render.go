// Package render contains pure, terminal-safe presentation of session records.
package render

import (
	"maps"
	"strings"
	"unicode"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

type Options struct {
	ShowToolActivity bool // Live-only recent-call card; never enabled for saved history alone.
	activityGroup    string
	Width            int
	MessageCount     int
	// AssistantTitle is the current model's display name (or ID).
	AssistantTitle            string
	AssistantStats            map[string]string // Frozen working metrics keyed by a block in the bubble.
	ExpandTools, ShowThinking bool
	// An explicit entry overrides ExpandTools, permitting individual collapse.
	Expanded map[string]bool
}

// ContentSpan identifies selectable content cells on a rendered line. A false
// Content marks frame decoration; an empty true span preserves a blank body row.
type ContentSpan struct {
	Start, End int
	Content    bool
}

type Document struct {
	ActivityRows []int // Heading rows animated by the UI without rebuilding the transcript.
	ImageLinks   []ImageLink
	Content      []ContentSpan
	Text         string
	Blocks       []Block
	MessageCount int
}

// Start is inclusive and End exclusive, in zero-based terminal lines of Text.
// Tool blocks inside an assistant bubble have their own non-overlapping ranges.
type Block struct {
	ID, Role string
	// InspectorParent/Task identify an isolated Worker task independently of
	// transcript positions or the mutable order of snapshot arrays.
	InspectorParent, InspectorTask string
	MessageIndex                   int
	Start, End                     int
	Foldable                       bool
	// Expanded is the effective state after global and per-ID overrides.
	// toolGroup ranges cover only their header; children have separate ranges.
	Expanded bool
}

// Clean removes all terminal commands and control characters from untrusted
// records before Markdown parsing. Renderer-generated SGR is never fed into it.
func Clean(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == '\u202e' || r == '\u202d' || (r >= '\u2066' && r <= '\u2069') {
			return -1
		}
		return r
	}, s)
}
func paint(s, color string) string {
	if color == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}
func oneLine(s string) string { return strings.Join(strings.Fields(Clean(s)), " ") }
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
func wrap(s string, width int) []string { return lines(ansi.Hardwrap(s, max(1, width), true)) }

func markdown(text string, width int, theme config.Theme) []string {
	text = Clean(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	style := styles.DarkStyleConfig
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix, style.Document.BlockSuffix = "", ""
	style.Document.Color = &theme.Text
	style.CodeBlock.Margin = &zero
	style.CodeBlock.Theme = ""
	style.CodeBlock.Chroma = &gansi.Chroma{
		Text:          gansi.StylePrimitive{Color: &theme.Text},
		Comment:       gansi.StylePrimitive{Color: &theme.Muted},
		Keyword:       gansi.StylePrimitive{Color: &theme.Warning},
		NameFunction:  gansi.StylePrimitive{Color: &theme.Accent},
		LiteralString: gansi.StylePrimitive{Color: &theme.Success},
		LiteralNumber: gansi.StylePrimitive{Color: &theme.Accent},
	}
	style.Heading.Color, style.Link.Color, style.LinkText.Color = &theme.Accent, &theme.Muted, &theme.Accent
	style.H1.BackgroundColor, style.H1.Color = nil, &theme.Accent
	style.Code.BackgroundColor, style.Code.Color = nil, &theme.Accent
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(style), glamour.WithWordWrap(max(1, width)), glamour.WithTableWrap(true))
	if err != nil {
		return visualWrap(text, width)
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return visualWrap(text, width)
	}
	// A second, ANSI-aware hard wrap handles unbroken code, URLs, wide tables,
	// and graphemes that the Markdown renderer cannot soft-wrap.
	return visualWrap(strings.Trim(rendered, "\n"), width)
}

// compactBubble removes only end-of-row layout padding (not leading/code
// indentation). ANSI-aware truncation retains generated colors and resets.
func compactBubble(body []string, title string, available int) ([]string, int) {
	rows := make([]string, len(body))
	width := ansi.StringWidth(title) + 6
	for i, row := range body {
		visible := strings.TrimRight(ansi.Strip(row), " \t")
		cells := ansi.StringWidth(visible)
		if cells > 0 {
			rows[i] = ansi.Truncate(row, cells, "")
		}
		width = max(width, cells+4)
	}
	return rows, min(available, width)
}

func frame(body []string, title string, width int, kind string, right bool, available int, themes ...config.Theme) []string {
	rows, _ := frameContent(body, title, width, kind, right, available, themes...)
	return rows
}

func frameContent(body []string, title string, width int, kind string, right bool, available int, themes ...config.Theme) ([]string, []ContentSpan) {
	width = max(4, width)
	title = fit(oneLine(title), width-6)
	middle := strings.Repeat("─", width-2)
	if title != "" {
		middle = "─ " + title + " " + strings.Repeat("─", max(0, width-5-ansi.StringWidth(title)))
	}
	head := FrameBorder(kind, "topLeft", "╭") + FrameBorder(kind, "top", middle) + FrameBorder(kind, "topRight", "╮")
	out := []string{head}
	spans := []ContentSpan{{}}
	left := 2
	if right {
		left += max(0, available-width)
	}
	for _, row := range body {
		// body already wrapped; preserve all rows rather than truncating content.
		for _, part := range wrap(row, width-4) {
			spans = append(spans, ContentSpan{Start: left, End: left + ansi.StringWidth(strings.TrimRight(ansi.Strip(part), " ")), Content: true})
			out = append(out, FrameBorder(kind, "left", "│")+" "+part+strings.Repeat(" ", max(0, width-4-ansi.StringWidth(part)))+" "+FrameBorder(kind, "right", "│"))
		}
		if row == "" {
			spans = append(spans, ContentSpan{Start: left, End: left, Content: true})
			out = append(out, FrameBorder(kind, "left", "│")+strings.Repeat(" ", width-2)+FrameBorder(kind, "right", "│"))
		}
	}
	out = append(out, FrameBorder(kind, "bottomLeft", "╰")+FrameBorder(kind, "bottom", strings.Repeat("─", width-2))+FrameBorder(kind, "bottomRight", "╯"))
	out = gradientFrame(kind, out, width, themes...)
	if right {
		for i := range out {
			out[i] = strings.Repeat(" ", max(0, available-width)) + out[i]
		}
	}
	spans = append(spans, ContentSpan{})
	return out, spans
}

func isExpanded(id string, opts Options) bool {
	if value, exists := opts.Expanded[id]; exists {
		return value
	}
	return opts.ExpandTools
}

func toolBody(item session.Item, theme config.Theme, opts Options, width int) []string {
	if visualName(item.ToolName) == "worker" {
		rows, _ := workerLayout(item, theme, opts, width)
		return rows
	}
	if name := visualName(item.ToolName); name == "write" || name == "edit" {
		width = min(width, changeContentWidth+4) // Preserve the existing frame padding.
	}
	expanded := isExpanded(item.ID, opts)
	out := []string{toolCallHeader(item, expanded, theme, width)}
	if !expanded {
		return out
	}
	contentWidth := width
	if width >= 8 {
		contentWidth = width - 4
	}
	detail := toolDetailRows(item, theme, contentWidth)
	if width >= 8 {
		name := oneLine(item.ToolName)
		if name == "" {
			name = "tool"
		}
		if visualName(item.ToolName) == "codemode" {
			// Its trusted styled title is built separately. Keep the border neutral
			// even on errors; the original error state still colors status/output.
			frameTheme, frameItem := theme, item
			frameTheme.Border = "#71899f"
			frameItem.Error = false
			detail = codemodeFrame(detail, frameItem, frameTheme, width)
		} else {
			cardWidth := width
			if tool := visualName(item.ToolName); tool == "read" || tool == "bash" || tool == "edit" || tool == "write" || tool == "ask_question" || genericTool(tool) {
				detail, cardWidth = compactBubble(detail, name, width)
			}
			detail = frame(detail, name, cardWidth, "tool", false, width, theme)
		}
	}
	return append(out, detail...)
}

func systemSynopsis(text string) string {
	text = Clean(text)
	for _, prefix := range []string{"Context compacted — earlier messages retained", "Branch summary"} {
		if strings.HasPrefix(text, prefix) {
			label := prefix
			if strings.HasPrefix(prefix, "Context") {
				label = "Context compacted"
			}
			rest := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			// Skip Markdown-only headings rather than spend the row on 'Summary'.
			for _, line := range strings.Split(rest, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") || line == "" {
					continue
				}
				return label + " · " + oneLine(line)
			}
			return label
		}
	}
	return oneLine(text)
}

// Transcript displays every visible record. Adjacent assistant text and tools
// share one left-aligned bubble; user messages are right-aligned and narrower.
// Hidden thinking and collapsed tool output remain untouched in the input data.
func Transcript(items []session.Item, theme config.Theme, opts Options) Document {
	opts.activityGroup = toolActivityGroup(items, opts)
	return transcript(items, theme, opts, markdown)
}

func transcript(items []session.Item, theme config.Theme, opts Options, renderMarkdown func(string, int, config.Theme) []string) Document {
	if opts.Width <= 0 {
		return Document{}
	}
	width := opts.Width
	assistantTitle := oneLine(opts.AssistantTitle)
	if assistantTitle == "" {
		assistantTitle = "助手"
	}
	messageCount := opts.MessageCount
	for _, item := range items {
		messageCount = max(messageCount, item.MessageIndex)
	}
	items = visibleItems(items, opts)
	var out []string
	var imageLinks []ImageLink
	var activityRows []int
	var blocks []Block
	var content []ContentSpan
	var sectionContent []ContentSpan
	appendSection := func(rows []string, sectionBlocks []Block) {
		if len(rows) == 0 {
			return
		}
		offset := len(out)
		if sectionContent == nil {
			for _, row := range rows {
				sectionContent = append(sectionContent, ContentSpan{End: ansi.StringWidth(strings.TrimRight(ansi.Strip(row), " ")), Content: true})
			}
		}
		content = append(content, sectionContent...)
		sectionContent = nil
		out = append(out, rows...)
		for _, b := range sectionBlocks {
			b.Start += offset
			b.End += offset
			blocks = append(blocks, b)
		}
	}
	for i := 0; i < len(items); {
		item := items[i]
		if item.Role == "assistant" || item.Role == "tool" || item.Role == "toolResult" || item.Role == "thinking" {
			start := i
			inner := max(1, width-4)
			var body []string
			var group []Block
			activityRow := -1
			afterToolGroup := false
			appendTool := func(it session.Item, available, indent int, childOpts Options) {
				rows, childBlocks := toolLayout(it, theme, childOpts, available)
				offset := len(body)
				for _, row := range rows {
					body = append(body, strings.Repeat(" ", indent)+row)
				}
				for _, b := range childBlocks {
					b.Start += offset
					b.End += offset
					group = append(group, b)
				}
			}
			for i < len(items) {
				it := items[i]
				if it.Role != "assistant" && it.Role != "tool" && it.Role != "toolResult" && it.Role != "thinking" {
					break
				}
				i++
				var rows []string
				tool := it.Role == "tool" || it.Role == "toolResult"
				if tool {
					end := i
					for end < len(items) && (items[end].Role == "tool" || items[end].Role == "toolResult") {
						end++
					}
					run := items[i-1 : end]
					id := "tool-group:" + it.ID
					expanded := isExpanded(id, opts)
					activity := id == opts.activityGroup
					if len(body) > 0 && strings.TrimSpace(ansi.Strip(body[len(body)-1])) != "" {
						body = append(body, "")
					}
					start := len(body)
					body = append(body, toolGroupHeader(run, expanded, theme, inner, activity))
					index := 0
					if !expanded {
						for _, child := range run {
							index = max(index, child.MessageIndex)
						}
					}
					group = append(group, Block{ID: id, Role: "toolGroup", MessageIndex: index, Start: start, End: start + 1, Foldable: true, Expanded: expanded})
					if expanded {
						for position, child := range run {
							childOpts := opts
							if _, explicit := opts.Expanded[child.ID]; activity && toolActivityDefaultExpanded(child) && !explicit {
								childOpts.Expanded = maps.Clone(opts.Expanded)
								if childOpts.Expanded == nil {
									childOpts.Expanded = map[string]bool{}
								}
								childOpts.Expanded[child.ID] = position >= len(run)-3
							}
							indent := min(2, max(0, inner-1))
							appendTool(child, inner-indent, indent, childOpts)
						}
					}
					if activity {
						activityRow = start
						if !expanded {
							rows, activityBlocks := toolActivityLayout(run, theme, opts, inner)
							for _, block := range activityBlocks {
								block.Start += len(body)
								block.End += len(body)
								group = append(group, block)
							}
							body = append(body, rows...)
						}
					}
					afterToolGroup = true
					i = end
					continue
				} else {
					thinking := it.Thinking
					if it.Role == "thinking" {
						thinking = it.Text
					}
					if opts.ShowThinking && thinking != "" {
						rows = append(rows, paint("thinking", theme.Thinking))
						for _, r := range wrap(Clean(thinking), inner) {
							rows = append(rows, paint(r, theme.Thinking))
						}
					}
					if it.Role != "thinking" {
						if len(rows) > 0 && it.Text != "" {
							rows = append(rows, "")
						}
						rows = append(rows, renderMarkdown(it.Text, inner, theme)...)
					}
					if it.Error {
						rows = append(rows, paint("Response ended with an error", theme.Error))
					}
				}
				if len(rows) == 0 {
					continue
				}
				// Separate the whole tool group from following prose, never its
				// header from its children.
				if afterToolGroup {
					body = append(body, "")
				}
				afterToolGroup = false
				start := len(body)
				body = append(body, rows...)
				group = append(group, Block{ID: it.ID, Role: it.Role, MessageIndex: it.MessageIndex, Start: start, End: len(body), Foldable: tool, Expanded: tool && isExpanded(it.ID, opts)})
			}
			if len(body) > 0 {
				if width >= 8 {
					var bubbleWidth int
					title := assistantTitle
					for n := i - 1; n >= start; n-- {
						if stats := opts.AssistantStats[items[n].ID]; stats != "" {
							title += " · " + oneLine(stats)
							break
						}
					}
					body, bubbleWidth = compactBubble(body, title, width)
					body, sectionContent = frameContent(body, title, bubbleWidth, "assistant", false, width, theme)
					if activityRow >= 0 {
						activityRow++
					}
					for n := range group {
						group[n].Start++
						group[n].End++
					}
				}
				if activityRow >= 0 {
					activityRows = append(activityRows, len(out)+activityRow)
				}
				appendSection(body, group)
			}
			continue
		}
		i++
		if item.Role == "user" {
			bubbleWidth := width
			if width >= 20 {
				bubbleWidth = width * 4 / 5
			}
			body := renderMarkdown(item.Text, max(1, bubbleWidth-4), theme)
			if len(body) == 0 {
				continue
			}
			if width >= 8 {
				title := "你"
				body, bubbleWidth = compactBubble(body, title, bubbleWidth)
				body, sectionContent = frameContent(body, title, bubbleWidth, "user", true, width, theme)
			}
			var links []ImageLink
			body, links = imageRows(body, item.ID, len(item.Images), len(out))
			imageLinks = append(imageLinks, links...)
			appendSection(body, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(body)}})
		} else if item.Role == "system" || item.Role == "compaction" || item.Role == "branch_summary" {
			body := []string{paint(fit(systemSynopsis(item.Text), max(1, width-4)), theme.Muted)}
			if width >= 8 {
				var bubbleWidth int
				title := "系统"
				body, bubbleWidth = compactBubble(body, title, width)
				body, sectionContent = frameContent(body, title, bubbleWidth, "system", false, width, theme)
			} else {
				body = []string{paint(fit(systemSynopsis(item.Text), width), theme.Muted)}
			}
			appendSection(body, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(body)}})
		} else {
			title := oneLine(item.Role)
			if title == "" {
				title = "notice"
			}
			body := wrap(paint("─ "+title+" ─", theme.Muted), width)
			body = append(body, wrap(Clean(item.Text), width)...)
			appendSection(body, []Block{{ID: item.ID, Role: item.Role, MessageIndex: item.MessageIndex, Start: 0, End: len(body)}})
		}
	}
	return Document{ActivityRows: activityRows, Text: strings.Join(out, "\n"), Blocks: blocks, Content: content, ImageLinks: imageLinks, MessageCount: messageCount}
}
