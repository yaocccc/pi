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

// Each tab has at most one in-flight layout. The job owns its cache until its
// result is delivered; synchronous layout can only use the tab's separate cache.
// Inputs are display copies, not references to mutable session/UI state.
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
	t.renderCache = render.TranscriptCache{}
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
	t.doc, t.docLinesText, t.docLines = v.doc, v.doc.Text, v.lines
	m.finishDocument(t)
	t.dirty = t.revision != v.job.revision
	t.streamDirty = t.dirty
}

func (m *Model) finishDocument(t *tab) {
	// A side-by-side recording retains the parent's immutable selection snapshot.
	if t.selection.document != t.doc.Text && !((m.inspector.view().Conversation || m.inspector.parent != nil) && m.inspector.tabID == t.id) {
		t.selection = transcriptSelection{}
	}
	m.clampScroll(t)
}
