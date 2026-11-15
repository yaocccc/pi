//go:build !unix

package runtime

import (
	"errors"
	"os/exec"
)

type SessionLease struct{}

func ProbeSession(agentDir, path string) (SessionStatus, error) {
	if path == "" {
		return SessionStatus{}, nil
	}
	return SessionStatus{}, errors.New("persistent sessions require Unix file locking on this platform")
}
func AcquireSession(agentDir, path string) (*SessionLease, error) {
	return nil, errors.New("persistent sessions require Unix file locking on this platform")
}
func (l *SessionLease) Close() error { return nil }
func (l *SessionLease) attach(cmd *exec.Cmd) error {
	return errors.New("session lock inheritance is unsupported")
}
func (l *SessionLease) started(pid int) error                  { return nil }
func (l *SessionLease) finish(clean bool, reason string) error { return nil }

func (l *SessionLease) prepareFile() error {
	return errors.New("persistent sessions require Unix file locking")
}
func (l *SessionLease) abortStart() error { return nil }
