//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package web

import (
	"context"
	"errors"
	"os"
)

func checkAttachTerminal(_, _ *os.File) error {
	return errors.New("shared desktop terminals require Unix PTYs; use WSL on Windows")
}
func attach(_ context.Context, _ AttachOptions) (AttachResult, error) {
	return AttachResult{}, checkAttachTerminal(nil, nil)
}
func waitForRetake(_ context.Context, _ *os.File) (bool, error) {
	return false, checkAttachTerminal(nil, nil)
}
