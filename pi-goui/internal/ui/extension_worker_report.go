package ui

import (
	"pi-goui/internal/extensions/hostui"
	"strings"
)

var workerReportItems = hostui.WorkerReportItems
var workerReportMarkdown = hostui.WorkerReportMarkdown

func workerReportFence(out *strings.Builder, language, text string) {
	hostui.WorkerReportFence(out, language, text)
}
