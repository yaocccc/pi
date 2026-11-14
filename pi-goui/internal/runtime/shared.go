package runtime

import (
	"context"
	"sync/atomic"
)

var sharingEnabled atomic.Bool

// EnableSessionSharing is called only by interactive CLI entry points. Checks
// and library callers remain direct; a helper never recursively shares itself.
func EnableSessionSharing() { sharingEnabled.Store(sharedSupported()) }

func RunSharedSessionHelper(args []string) (bool, error) { return runSharedHelper(args) }

// Shared reports whether this client is attached to a cross-process backend.
func (c *Client) Shared() bool { return c.shared != nil }

// BeginFileMutation excludes other attachments and stops the sole backend
// before granting direct file access. Its independent connection retains the
// lease even after Client.Close, until release or context cancellation.
func (c *Client) BeginFileMutation(ctx context.Context) (func(), error) {
	if c.shared == nil {
		return func() {}, nil
	}
	return c.shared.mutation(ctx)
}
