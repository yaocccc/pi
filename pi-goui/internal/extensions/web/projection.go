// Package web owns optional Web-extension display policies.
package web

// HideMessage suppresses agent-only background notifications, not their native
// records or message ordinals. Unknown custom message types remain visible.
func HideMessage(role, customType string) bool {
	return role == "custom" && customType == "web-search-content-ready"
}
