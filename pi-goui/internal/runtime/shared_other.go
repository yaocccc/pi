//go:build !unix

package runtime

import "context"

// Non-Unix platforms explicitly retain direct per-TUI backends.
type SessionMember struct{}

func JoinSessionMember(string) (*SessionMember, error) { return &SessionMember{}, nil }
func (*SessionMember) Close() error                    { return nil }

type sharedConnection struct{ pid int }

func (*sharedConnection) mutation(context.Context) (func(), error) { return func() {}, nil }
func sharedSupported() bool                                        { return false }
func startShared(cfg Config) (*Client, error)                      { return startDirect(cfg) }
func runSharedHelper(args []string) (bool, error)                  { return false, nil }
