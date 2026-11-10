package session

import "strings"

// DisplayName is presentation-only; it never renames the saved session.
func DisplayName(state State) string {
	if strings.TrimSpace(state.Name) != "" {
		return state.Name
	}
	for _, item := range state.Items {
		if item.Role == "user" && strings.TrimSpace(item.Text) != "" {
			return item.Text
		}
	}
	return ""
}
