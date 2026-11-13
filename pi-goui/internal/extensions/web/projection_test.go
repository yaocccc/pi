package web

import "testing"

func TestMessagePolicyIsNarrow(t *testing.T) {
	for _, tc := range []struct {
		role, kind string
		hidden     bool
	}{
		{"custom", "web-search-content-ready", true},
		{"user", "web-search-content-ready", false},
		{"custom", "other-extension-message", false},
		{"custom", "", false},
	} {
		if got := HideMessage(tc.role, tc.kind); got != tc.hidden {
			t.Fatalf("%q/%q hidden=%v", tc.role, tc.kind, got)
		}
	}
}
