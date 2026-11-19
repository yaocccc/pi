package ui

import (
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	rpc "pi-goui/internal/runtime"
)

// Layout invalidations (resize, folds, theme, history replacement) must not be
// confused with content updates. A wheel/blink event never flushes the latter.
func (t *tab) invalidateDocument() {
	t.dirty = true
	t.streamDirty = false
	t.layoutGeneration++
}

func (t *tab) invalidateStream() {
	if !t.dirty {
		t.streamDirty = true
	}
	t.dirty = true
}

// The published document is immutable. All viewport and hit-test consumers use
// this index, instead of scanning the entire transcript on every input/event.
func (t *tab) documentLines() []string {
	if t.docLines == nil || t.docLinesText != t.doc.Text {
		t.docLinesText = t.doc.Text
		t.docLines = strings.Split(t.doc.Text, "\n")
	}
	return t.docLines
}

func (m *Model) documentOptions(t *tab) render.Options {
	opts := conversationOptions(t.state, m.mainWidth(), t.expanded, t.showThinking, t.folds)
	opts.Tools = m.extensions.Tools
	opts.ShowToolActivity = opts.ShowToolActivity && !t.dead
	opts.AssistantStats = t.assistantStats
	return opts
}

// Each tab has at most one in-flight layout. The job has a private cache value
// seeded with immutable sections. A fold on the UI goroutine can still reuse
// those sections while the job computes replacement ones. Mutable inputs are copied.
type documentJob struct {
	id                   int
	client               *rpc.Client
	file, sessionID      string
	generation, revision uint64
	items                []session.Item
	theme                config.Theme
	opts                 render.Options
	cache                render.TranscriptCache
}
type documentResult struct {
	job   *documentJob
	cache render.TranscriptCache
	doc   render.Document
	lines []string
}

func (j *documentJob) render() tea.Msg {
	cache := j.cache
	doc := cache.Transcript(j.items, j.theme, j.opts)
	return documentResult{job: j, cache: cache, doc: doc, lines: strings.Split(doc.Text, "\n")}
}

func (m *Model) scheduleDocument(t *tab) tea.Cmd {
	if t.documentJob != nil || t.selection.dragging || m.panelResize.dragging {
		return nil
	}
	if m.showStartupInfo(t) {
		m.document(t)
		return nil
	}
	opts := m.documentOptions(t)
	opts.Expanded = maps.Clone(opts.Expanded)
	opts.AssistantStats = maps.Clone(opts.AssistantStats)
	items := slices.Clone(t.state.Items)
	for i := range items {
		items[i].Arguments = slices.Clone(items[i].Arguments)
		items[i].Details = slices.Clone(items[i].Details)
		items[i].Images = slices.Clone(items[i].Images)
		if items[i].Compaction != nil {
			value := *items[i].Compaction
			items[i].Compaction = &value
		}
	}
	job := &documentJob{id: t.id, client: t.client, file: t.state.File, sessionID: t.state.ID,
		generation: t.layoutGeneration, revision: t.revision, items: items, theme: m.cfg.Theme, opts: opts, cache: t.renderCache}
	t.documentJob = job
	return job.render
}

func (m *Model) receiveDocument(v documentResult) {
	t := m.byID(v.job.id)
	if t == nil || t.documentJob != v.job {
		return
	}
	t.documentJob = nil
	if t.layoutGeneration != v.job.generation || t.client != v.job.client || t.state.File != v.job.file || t.state.ID != v.job.sessionID || m.mainWidth() != v.job.opts.Width || m.cfg.Theme != v.job.theme || m.extensions.Tools != v.job.opts.Tools {
		return
	}
	t.renderCache = v.cache
	if t.selection.dragging || m.panelResize.dragging {
		return
	}
	// Publish compatible snapshots even when more deltas arrived while rendering.
	// Rejecting every older content revision would starve a continuous stream.
	m.publishDocument(t, v.doc, v.lines)
	t.dirty = t.revision != v.job.revision
	t.streamDirty = t.dirty
}

func (m *Model) publishDocument(t *tab, doc render.Document, lines []string) {
	t.nativeDocument = doc
	if t.promptEcho != nil {
		doc = m.echoDocument(t, doc)
		lines = nil
	}
	// Refreshes may insert rows above the viewport. Anchor the existing view
	// to its stable message/tool ID instead of keeping only a raw row offset.
	anchor, within := "", 0
	if !t.follow {
		for _, block := range t.doc.Blocks {
			if block.ID != "" && block.Start <= t.offset && t.offset < block.End {
				anchor, within = block.ID, t.offset-block.Start
			}
		}
	}
	t.doc = doc
	t.docLinesText, t.docLines = doc.Text, lines
	if anchor != "" {
		for _, block := range doc.Blocks {
			if block.ID == anchor {
				t.offset = block.Start + min(within, max(0, block.End-block.Start-1))
				break
			}
		}
	}
	// A side-by-side recording retains the parent's immutable selection snapshot.
	if t.selection.document != t.doc.Text && !((m.inspector.view().Conversation || m.inspector.parent != nil) && m.inspector.tabID == t.id) {
		selection := &t.selection
		a, b := selection.ordered()
		oldLines := selection.lines
		if oldLines == nil {
			oldLines = strings.Split(selection.document, "\n")
		}
		newLines := t.documentLines()
		unchanged := selection.moved && a.line >= 0 && b.line < len(oldLines) && b.line < len(newLines)
		for row := a.line; unchanged && row <= b.line; row++ {
			unchanged = oldLines[row] == newLines[row]
			if row < len(selection.content) && row < len(doc.Content) {
				unchanged = unchanged && selection.content[row] == doc.Content[row]
			}
		}
		if unchanged {
			selection.document, selection.lines, selection.content = doc.Text, newLines, doc.Content
		} else {
			*selection = transcriptSelection{}
		}
	}
	m.clampScroll(t)
}
