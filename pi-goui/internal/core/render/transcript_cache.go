package render

import (
	"maps"
	"slices"
	"strings"

	"pi-goui/internal/core/config"
	"pi-goui/internal/core/session"
)

// TranscriptCache belongs to one tab. Its published sections (including their
// documents, keys and Markdown rows) are immutable: a value copy may seed a
// concurrent layout job. Transcript only updates the receiving cache. It retains
// only the latest document's sections, not every version of streaming output.
// Completed bubbles reuse their layout; a changing assistant bubble additionally
// reuses Markdown for its unchanged messages before recomputing its frame.
type TranscriptCache struct {
	theme    config.Theme
	opts     Options
	sections []cachedSection
}

type cachedSection struct {
	items    []transcriptItemKey
	doc      Document
	markdown map[markdownKey][]string
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
	compaction                                                     session.CompactionMetrics
	hasCompaction                                                  bool
	assistantStats                                                 string
	activityStart                                                  bool
}

func itemKeys(items []session.Item, opts Options, old []transcriptItemKey) ([]transcriptItemKey, bool) {
	var next []transcriptItemKey
	if len(old) != len(items) {
		next = make([]transcriptItemKey, len(items))
	}
	activeID, hasActivity := strings.CutPrefix(opts.activityGroup, "tool-group:")
	for i, it := range items {
		var prev transcriptItemKey
		if i < len(old) {
			prev = old[i]
		}
		args, details := it.LayoutJSONKeys(prev.arguments, prev.details)
		key := transcriptItemKey{
			id: it.ID, role: it.Role, text: it.Text, thinking: it.Thinking, toolID: it.ToolID, toolName: it.ToolName,
			arguments: args, details: details, running: it.Running, failed: it.Error, hasResult: it.HasResult,
			estimatedTokens: it.EstimatedTokens, messageIndex: it.MessageIndex, imageCount: len(it.Images), hasCompaction: it.Compaction != nil,
			// Both are local to a bubble: do not flush history when the latest
			// tool's live preview or a completed turn's stats change.
			assistantStats: opts.AssistantStats[it.ID], activityStart: hasActivity && it.ID == activeID && (it.Role == "tool" || it.Role == "toolResult"),
		}
		if it.Compaction != nil {
			key.compaction = *it.Compaction
		}
		if next == nil && key != prev {
			next = slices.Clone(old)
		}
		if next != nil {
			next[i] = key
		}
	}
	if next == nil {
		return old, false
	}
	return next, true
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

// A fold override addresses a rendered block, including tool-group headers and
// nested extension blocks. Hidden children pick up their current overrides when
// a parent opens. Compare presence too: an explicit false can suppress a live
// preview's per-child default. Both maps are immutable during this layout.
func sectionFoldsChanged(blocks []Block, before, after map[string]bool) bool {
	for _, b := range blocks {
		old, had := before[b.ID]
		next, has := after[b.ID]
		if old != next || had != has {
			return true
		}
	}
	return false
}

// Transcript is output-equivalent to the uncached Transcript function. Width,
// theme and global presentation changes invalidate the cache. Per-ID fold
// changes invalidate only their section. MessageCount is metadata.
func (c *TranscriptCache) Transcript(items []session.Item, theme config.Theme, opts Options) Document {
	if opts.Width <= 0 {
		*c = TranscriptCache{}
		return Document{}
	}
	opts.activityGroup = toolActivityGroup(items, opts)
	previousFolds := c.opts.Expanded
	foldsChanged := !maps.Equal(previousFolds, opts.Expanded)
	if c.opts.Tools != opts.Tools || c.theme != theme || c.opts.Width != opts.Width || c.opts.AssistantTitle != opts.AssistantTitle || c.opts.ShowThinking != opts.ShowThinking || c.opts.ExpandTools != opts.ExpandTools {
		c.sections = nil
	}
	c.theme, c.opts = theme, opts
	if foldsChanged {
		c.opts.Expanded = maps.Clone(opts.Expanded)
	} else {
		c.opts.Expanded = previousFolds
	}
	c.opts.AssistantStats = nil // Immutable values are snapshotted in the item keys.
	count := opts.MessageCount
	for _, it := range items {
		count = max(count, it.MessageIndex)
	}
	items = visibleItems(items, opts)
	var cold map[markdownKey]*coldMarkdown
	if len(c.sections) == 0 && len(items) >= 128 {
		cold = prepareColdMarkdown(items, opts.Width, theme)
	}
	next := make([]cachedSection, 0, len(c.sections))
	blockCount := 0
	for start := 0; start < len(items); {
		end := start + 1
		if assistantItem(items[start]) {
			for end < len(items) && assistantItem(items[end]) {
				end++
			}
		}
		var section cachedSection
		if len(next) < len(c.sections) {
			section = c.sections[len(next)]
		}
		keys, itemsChanged := itemKeys(items[start:end], opts, section.items)
		if itemsChanged || (foldsChanged && sectionFoldsChanged(section.doc.Blocks, previousFolds, opts.Expanded)) {
			used := make(map[markdownKey][]string)
			cachedMarkdown := func(body string, width int, theme config.Theme) []string {
				key := markdownKey{body, width}
				rows, ok := used[key]
				if !ok {
					rows, ok = section.markdown[key]
				}
				if !ok {
					if prepared := cold[key]; prepared != nil {
						rows = prepared.rows
					} else {
						rows = markdown(body, width, theme)
					}
				}
				used[key] = rows
				return rows
			}
			section.doc = transcript(items[start:end], theme, opts, cachedMarkdown)
			section.items, section.markdown = keys, used
		}
		next = append(next, section)
		blockCount += len(section.doc.Blocks)
		start = end
	}
	idx := &rowIndex{parts: make([]rowPart, 0, len(next))}
	blocks := slices.Grow([]Block(nil), blockCount)
	var targets []messageTarget
	var imageLinks []ImageLink
	var activityRows []int
	lines := 0
	for _, section := range next {
		if section.doc.ByteLen() != 0 || len(section.doc.Blocks) > 0 {
			idx.append(section.doc.DocumentRows, lines)
			targets = appendMessageTargets(targets, section.doc.messageTargets, lines, 0)
			for _, row := range section.doc.ActivityRows {
				activityRows = append(activityRows, row+lines)
			}
			for _, link := range section.doc.ImageLinks {
				link.Row += lines
				imageLinks = append(imageLinks, link)
			}
			for _, block := range section.doc.Blocks {
				block.Start += lines
				block.End += lines
				blocks = append(blocks, block)
			}
			lines += section.doc.LineCount()
		}
	}
	c.sections = next
	if lines == 0 {
		idx = nil
	}
	return Document{messageTargets: targets, ActivityRows: activityRows, DocumentRows: DocumentRows{idx}, Blocks: blocks, ImageLinks: imageLinks, MessageCount: count}
}
