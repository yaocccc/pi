package render

import (
	"maps"
	"slices"
	"strings"

	"pi-goui/internal/config"
	"pi-goui/internal/session"
)

// TranscriptCache belongs to one tab and is used on the UI goroutine. It retains
// only the latest document's sections, not every version of streaming output.
// Completed bubbles reuse their layout; a changing assistant bubble additionally
// reuses Markdown for its unchanged messages before recomputing its frame.
type TranscriptCache struct {
	theme    config.Theme
	opts     Options
	sections []cachedSection
}

type cachedSection struct {
	followingProse bool
	items          []transcriptItemKey
	doc            Document
	markdown       map[markdownKey][]string
}

type markdownKey struct {
	text  string
	width int
}

// Copy RawMessages to strings so in-place updates cannot mutate saved keys.
// Include presentation fields, but only the count of immutable saved images:
// links resolve current bytes by item ID/index, so never copy/hash base64 here.
// Status, result metadata and message ordinals also
// affect tool rendering and navigation, even when the visible text is unchanged.
type transcriptItemKey struct {
	id, role, text, thinking, toolID, toolName, arguments, details string
	running, failed, hasResult                                     bool
	estimatedTokens, messageIndex, imageCount                      int
}

func itemKeys(items []session.Item) []transcriptItemKey {
	keys := make([]transcriptItemKey, len(items))
	for i, it := range items {
		keys[i] = transcriptItemKey{it.ID, it.Role, it.Text, it.Thinking, it.ToolID, it.ToolName, string(it.Arguments), string(it.Details), it.Running, it.Error, it.HasResult, it.EstimatedTokens, it.MessageIndex, len(it.Images)}
	}
	return keys
}

func visibleItems(items []session.Item, opts Options) []session.Item {
	visible := make([]session.Item, 0, len(items))
	for _, item := range items {
		// Tool-only assistant messages have no visible body, but still occur
		// between tool batches. Do not let these invisible records split runs.
		if !item.Error {
			switch item.Role {
			case "thinking":
				if !opts.ShowThinking || strings.TrimSpace(item.Text) == "" {
					continue
				}
			case "assistant":
				if strings.TrimSpace(item.Text) == "" && (!opts.ShowThinking || strings.TrimSpace(item.Thinking) == "") {
					continue
				}
			}
		}
		visible = append(visible, item)
	}
	return visible
}

func assistantItem(item session.Item) bool {
	return item.Role == "assistant" || item.Role == "tool" || item.Role == "toolResult" || item.Role == "thinking"
}

// Transcript is output-equivalent to the uncached Transcript function. Width,
// theme and presentation changes invalidate the cache, including in-place edits
// to Expanded. MessageCount is metadata and does not invalidate bubble layouts.
func (c *TranscriptCache) Transcript(items []session.Item, theme config.Theme, opts Options) Document {
	if opts.Width <= 0 {
		*c = TranscriptCache{}
		return Document{}
	}
	if c.theme != theme || c.opts.Width != opts.Width || c.opts.AssistantTitle != opts.AssistantTitle || c.opts.ShowThinking != opts.ShowThinking || c.opts.ExpandTools != opts.ExpandTools || !maps.Equal(c.opts.Expanded, opts.Expanded) || !maps.Equal(c.opts.AssistantStats, opts.AssistantStats) {
		c.sections = nil
	}
	c.theme, c.opts = theme, opts
	c.opts.Expanded = maps.Clone(opts.Expanded)
	c.opts.AssistantStats = maps.Clone(opts.AssistantStats)
	count := opts.MessageCount
	for _, it := range items {
		count = max(count, it.MessageIndex)
	}
	items = visibleItems(items, opts)
	next := make([]cachedSection, 0, len(c.sections))
	textBytes := 0
	for start := 0; start < len(items); {
		end := start + 1
		if assistantItem(items[start]) {
			for end < len(items) && assistantItem(items[end]) {
				end++
			}
		}
		sectionOpts := opts
		for _, following := range items[end:] {
			if following.Role != "tool" && following.Role != "toolResult" && following.Role != "thinking" && strings.TrimSpace(following.Text) != "" {
				sectionOpts.followingProse = true
				break
			}
		}
		keys := itemKeys(items[start:end])
		var section cachedSection
		if len(next) < len(c.sections) {
			section = c.sections[len(next)]
		}
		if !slices.Equal(section.items, keys) || section.followingProse != sectionOpts.followingProse {
			used := make(map[markdownKey][]string)
			cachedMarkdown := func(body string, width int, theme config.Theme) []string {
				key := markdownKey{body, width}
				rows, ok := used[key]
				if !ok {
					rows, ok = section.markdown[key]
				}
				if !ok {
					rows = markdown(body, width, theme)
				}
				used[key] = rows
				return rows
			}
			section.doc = transcript(items[start:end], theme, sectionOpts, cachedMarkdown)
			section.followingProse = sectionOpts.followingProse
			section.items, section.markdown = keys, used
		}
		next = append(next, section)
		textBytes += len(section.doc.Text) + 1
		start = end
	}
	var text strings.Builder
	text.Grow(textBytes)
	var blocks []Block
	var content []ContentSpan
	var imageLinks []ImageLink
	lines := 0
	for _, section := range next {
		if section.doc.Text != "" || len(section.doc.Blocks) > 0 {
			if lines > 0 {
				text.WriteByte('\n')
			}
			text.WriteString(section.doc.Text)
			content = append(content, section.doc.Content...)
			for _, link := range section.doc.ImageLinks {
				link.Row += lines
				imageLinks = append(imageLinks, link)
			}
			for _, block := range section.doc.Blocks {
				block.Start += lines
				block.End += lines
				blocks = append(blocks, block)
			}
			lines += strings.Count(section.doc.Text, "\n") + 1
		}
	}
	c.sections = next
	return Document{Text: text.String(), Blocks: blocks, Content: content, ImageLinks: imageLinks, MessageCount: count}
}
