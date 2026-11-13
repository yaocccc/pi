package hostui

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/config"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/core/render"
	"pi-goui/internal/core/session"
	"pi-goui/internal/extensions/toolrender/detail"
	"pi-goui/internal/extensions/worker"
	"strings"
	"time"
)

// Inspector owns extension interpretation, replay and fallback. The host owns
// only viewport/selection state and the lifetime of each content instance.
type Inspector struct {
	Tools                *render.ToolViews
	Worker               *WorkerSession
	Key, Title, Fallback string
	source               session.Item
	selected             session.Item
	projected, found     bool
	projectedKey         string
	revision             uint64
}
type InspectorView = extension.InspectorView

func NewInspector(item session.Item, key string, thinking bool, client worker.RPCClient, tools *render.ToolViews) (*Inspector, bool) {
	if key == "" {
		return nil, false
	}
	selected, title, found := detail.WorkerTaskResult(item, key)
	if !found {
		return nil, false
	}
	p := &Inspector{Key: key, Title: title, Tools: tools}
	p.Worker, p.Fallback = NewWorkerSession(selected, thinking, client)
	p.Resolve(item)
	return p, true
}
func (p *Inspector) Matches(item session.Item, key string) bool {
	selected, _, ok := detail.WorkerTaskResult(item, key)
	if !ok || p.Key != key || p.Worker == nil || p.Fallback != "" {
		return false
	}
	d, _, ok := SelectedWorkerDescriptor(selected)
	return ok && p.Worker.Descriptor.ID == d.ID
}
func (p *Inspector) Resolve(item session.Item) (session.Item, bool) {
	if p.Key == "" {
		return item, true
	}
	if p.projected && p.projectedKey == p.Key && p.source.ID == item.ID && p.source.Role == item.Role && p.source.ToolName == item.ToolName && p.source.HasResult == item.HasResult && p.source.Text == item.Text && p.source.Running == item.Running && p.source.Error == item.Error && bytes.Equal(p.source.Arguments, item.Arguments) && bytes.Equal(p.source.Details, item.Details) {
		return p.selected, p.found
	}
	selected, title, ok := detail.WorkerTaskResult(item, p.Key)
	p.source, p.selected, p.Title, p.projected, p.found, p.projectedKey = item, selected, title, true, ok, p.Key
	p.source.Arguments = bytes.Clone(item.Arguments)
	p.source.Details = bytes.Clone(item.Details)
	p.source.Images = nil
	p.revision++
	return selected, ok
}
func (p *Inspector) Version() uint64 {
	if p.Worker != nil {
		return p.revision + p.Worker.Revision
	}
	return p.revision
}
func (p *Inspector) unavailable(reason string) {
	p.Fallback = reason
	p.revision++
	if p.Worker != nil && !p.Worker.Unavailable(reason) {
		p.Worker = nil
	}
}
func (p *Inspector) Poll(item session.Item, now time.Time) tea.Cmd {
	if p.Worker == nil || p.Fallback != "" {
		return nil
	}
	cmd, reason := p.Worker.Poll(item, now)
	if reason != "" {
		p.unavailable(reason)
	}
	return cmd
}
func (p *Inspector) Update(msg tea.Msg, item *session.Item) (tea.Cmd, bool) {
	result, ok := msg.(WorkerTranscriptResult)
	if !ok || p.Worker == nil || result.Owner != p.Worker {
		return nil, false
	}
	if p.Fallback != "" {
		return nil, true
	}
	drain, reason := p.Worker.Receive(result, item)
	if reason != "" {
		p.unavailable(reason)
		return nil, true
	}
	if drain && item != nil {
		return p.Poll(*item, time.Now()), true
	}
	return nil, true
}
func IsInspectorMessage(msg tea.Msg) bool { _, ok := msg.(WorkerTranscriptResult); return ok }
func (p *Inspector) View() InspectorView {
	if p == nil {
		return InspectorView{}
	}
	v := InspectorView{Title: "worker · " + strings.ReplaceAll(render.Clean(p.Title), "\n", " ")}
	if p.Fallback != "" {
		v.Title = "简要详情 · " + strings.ReplaceAll(render.Clean(p.Title), "\n", " ")
	}
	if w := p.Worker; w != nil {
		v.Conversation = true
		v.Document = w.Doc
		v.Items = w.Visible
		v.Targets = w.Targets
		v.Title = "只读 · " + strings.ReplaceAll(render.Clean(p.Title), "\n", " ")
		v.Status = render.Clean(w.Replay.Status)
	}
	return v
}
func (p *Inspector) Rows(item session.Item, theme config.Theme, width int, options func(session.State, int, bool, bool, map[string]bool) render.Options, button string) []string {
	if p.Worker != nil {
		return p.Worker.Rows(theme, width, func(state session.State, width int, expanded, thinking bool, folds map[string]bool) render.Options {
			opts := options(state, width, expanded, thinking, folds)
			opts.Tools = p.Tools
			return opts
		}, button)
	}
	rows := func(item session.Item, theme config.Theme, width int) []string {
		return render.ToolResultRows(item, theme, width, p.Tools)
	}
	if p.Fallback == "" {
		return rows(item, theme, width)
	}
	return detail.WorkerTaskSummaryRows(item, p.Fallback, theme, width, render.StyledLines, render.Clean, rows)
}
func (p *Inspector) Activity(row int) bool {
	w := p.Worker
	if w == nil {
		return false
	}
	return w.ActivityRows[row]
}
func (p *Inspector) ToggleThinking() {
	if w := p.Worker; w != nil {
		w.ShowThinking = !w.ShowThinking
		w.Revision++
	}
}
func (p *Inspector) ToggleFold(row int) {
	w := p.Worker
	if w == nil {
		return
	}
	if w.Folds == nil {
		w.Folds = map[string]bool{}
	}
	for _, b := range w.Doc.Blocks {
		if b.Foldable && row >= b.Start && row < b.End {
			w.Folds[b.ID] = !b.Expanded
			w.Revision++
			return
		}
	}
}
func (p *Inspector) InputTarget(x, row int) (string, bool) {
	if w := p.Worker; w != nil {
		for _, a := range w.InputActions {
			if a.Rect.Contains(x, row) {
				return a.Key, true
			}
		}
	}
	return "", false
}
func (p *Inspector) ToggleInput(key string) {
	if w := p.Worker; w != nil {
		if w.InputExpanded == nil {
			w.InputExpanded = map[string]bool{}
		}
		w.InputExpanded[key] = !w.InputExpanded[key]
		w.Revision++
	}
}
func (p *Inspector) Child(id string) (string, extension.Inspector, bool) {
	w := p.Worker
	if w == nil {
		return "", nil, false
	}
	itemID, key := id, ""
	for _, b := range w.Doc.Blocks {
		if b.ID == id && b.InspectorTask != "" {
			itemID, key = b.InspectorParent, b.InspectorTask
			break
		}
	}
	for _, item := range w.Visible {
		if item.ID == itemID {
			if key == "" {
				return itemID, nil, true
			}
			child, ok := NewInspector(item, key, w.ShowThinking, w.Client, p.Tools)
			if !ok {
				return itemID, nil, false
			}
			return itemID, child, true
		}
	}
	return "", nil, false
}

// Targets for task collections belong to their adapter, not tool-name tests in UI.
func InspectableCollection(item session.Item) bool {
	return (item.Role == "tool" || item.Role == "toolResult") && (item.ToolName == "worker" || strings.HasSuffix(item.ToolName, ".worker"))
}
