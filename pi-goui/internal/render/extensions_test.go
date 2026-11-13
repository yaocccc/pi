package render

import (
	"pi-goui/internal/config"
	"pi-goui/internal/extensions/toolrender/cards"
	"pi-goui/internal/session"
)

// Existing host integration tests also exercise the extracted adapter helpers.
type workerCard = cards.WorkerCard

func workerData(item session.Item) cards.WorkerView { return extensionCards().WorkerData(item) }
func workerHeader(item session.Item, v cards.WorkerView, e bool, t config.Theme, w int) string {
	return extensionCards().WorkerHeader(item, v, e, t, w)
}

var workerDuration = cards.WorkerDuration
var workerSlotNotice = cards.WorkerSlotNotice
var workerTaskID = cards.WorkerTaskID

func workerThinkingSteps(s string) []string { return extensionCards().WorkerThinkingSteps(s) }
func workerToolRows(c workerCard, t config.Theme, w int) []string {
	return extensionCards().WorkerToolRows(c, t, w)
}
