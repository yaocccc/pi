package cards

import "testing"

func TestCodexModeLabels(t *testing.T) {
	for _, tc := range []struct{ mode, label string }{{"normal", "normal x1"}, {"fast", "fast x2.5"}, {"ultrafast", "ultrafast x8"}, {"", "normal x1"}, {"future", "normal x1"}} {
		if got := CodexModeLabel(tc.mode); got != tc.label {
			t.Errorf("mode %q: got %q, want %q", tc.mode, got, tc.label)
		}
	}
}
