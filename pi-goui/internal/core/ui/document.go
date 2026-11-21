package ui

import (
	"maps"
	"slices"

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
}

func (j *documentJob) render() tea.Msg {
	cache := j.cache
	doc := cache.Transcript(j.items, j.theme, j.opts)
	return documentResult{job: j, cache: cache, doc: doc}
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
		items[i] = items[i].LayoutSnapshot()
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
	m.publishDocument(t, v.doc)
	t.dirty = t.revision != v.job.revision
	t.streamDirty = t.dirty
	if t.originJump != nil && (t.originJump.awaitingLayout || t.resume.preview) {
		m.applyOriginJump(t)
	}
}

func (m *Model) publishDocument(t *tab, doc render.Document) {
	t.nativeDocument = doc
	if t.promptEcho != nil {
		doc = m.echoDocument(t, doc)
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
	if anchor != "" {
		for _, block := range doc.Blocks {
			if block.ID == anchor {
				t.offset = block.Start + min(within, max(0, block.End-block.Start-1))
				break
			}
		}
	}
	// A side-by-side recording retains the parent's immutable selection snapshot.
	if t.selection.document != t.doc.DocumentRows && !((m.inspector.view().Conversation || m.inspector.parent != nil) && m.inspector.tabID == t.id) {
		selection := &t.selection
		a, b := selection.ordered()
		old := selection.document
		unchanged := selection.moved && a.line >= 0 && b.line < old.LineCount() && b.line < doc.LineCount()
		for row := a.line; unchanged && row <= b.line; row++ {
			before, had := old.Span(row)
			after, has := doc.Span(row)
			unchanged = old.Line(row) == doc.Line(row) && had == has && before == after
		}
		if unchanged {
			selection.document = doc.DocumentRows
		} else {
			*selection = transcriptSelection{}
		}
	}
	m.clampScroll(t)
}
