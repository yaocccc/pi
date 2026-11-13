package ui

import "pi-goui/internal/extensions/hostui"

const workerContractPrefix = hostui.WorkerContractPrefix

type workerInputAction = hostui.WorkerInputAction

var workerInputMarkdown = hostui.WorkerInputMarkdown
var workerInputLists = hostui.WorkerInputLists
var workerInputItems = hostui.WorkerInputItems
var workerInputTargets = hostui.WorkerInputTargets

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
	for _, a := range w.InputActions {
		r := a.Rect
		r.X += left + inset
		r.Y += 2 - m.inspector.offset
		if r.Contains(x, y) {
			return a.Key, true
		}
	}
	return "", false
}
