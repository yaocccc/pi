package tooldetail

import (
	"pi-goui/internal/extensionapi"
	"pi-goui/internal/extensions/toolrender/detail"
	"pi-goui/internal/session"
)

// Only the generic host formatting boundary lives here; views live in adapters.
func (v *renderer) extensions() *detail.Renderer {
	return detail.New(extensionapi.DetailHost{Theme: v.theme, Width: v.width, Hooks: v.hooks, Rows: &v.rows,
		Line: v.line, Heading: v.heading, Output: v.output, Value: v.value, Field: v.field, Scalar: v.scalar})
}

type WorkerTask = detail.WorkerTask

func WorkerTasks(item session.Item) []WorkerTask { return detail.WorkerTasks(item) }
