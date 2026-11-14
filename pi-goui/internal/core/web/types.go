// Package web hosts the existing TUI in a browser terminal.
package web

// Config describes the TUI subprocess. The HTTP host never implements Pi RPC
// or duplicates the TUI's session and extension state. Env, when non-nil, is
// the complete subprocess environment; nil inherits the current environment.
type Config struct {
	Command             []string
	CommandForDirectory func(string) []string // Optional cwd-specific startup flags.
	// NewInstanceCommandForDirectory must omit any explicit session/resume flags
	// belonging to the root TUI. Without it, creating independent instances fails
	// closed rather than risking two writers to the same session.
	NewInstanceCommandForDirectory func(string) []string
	DiscoverDesktops               bool // Discover same-user private desktop endpoints.
	SharedDesktop                  bool
	OfferDefaultInstance           bool // Expose an unstarted root with explicit session options.
	Cwd                            string
	AgentDir                       string // Native session headers supply recent projects.
	Env                            []string
	Token                          string
}
