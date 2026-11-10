// Package codex reads the current official Codex OAuth account quota.
package codex

import "time"

// Identity must use the selected model's effective BaseURL from pi, including
// any model-level override; passing a provider default would bypass that gate.
type Identity struct{ Provider, BaseURL string }
type Snapshot struct {
	LoggedIn         bool
	RemainingPercent *float64
	ResetAt          time.Time
	Credits          *float64
	UnlimitedCredits bool
	FetchedAt        time.Time
}
