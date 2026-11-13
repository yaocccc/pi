package tooldetail

import (
	"pi-goui/internal/core/extensionapi"
	"pi-goui/internal/core/session"
)

// Provider renders an optional result using this call's host and rows.
// Return false to retain the generic fallback. No provider is installed globally.
type Provider func(extensionapi.DetailHost, string, session.Item, map[string]any, map[string]any) bool

func (v *renderer) extensionResult(name string, item session.Item, args, details map[string]any) bool {
	if v.provider == nil {
		return false
	}
	return v.provider(extensionapi.DetailHost{Theme: v.theme, Width: v.width, Hooks: v.hooks, Rows: &v.rows,
		Line: v.line, Heading: v.heading, Output: v.output, Value: v.value, Field: v.field, Scalar: v.scalar}, name, item, args, details)
}
