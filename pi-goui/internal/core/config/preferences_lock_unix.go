//go:build unix && !aix

package config

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// The lock inode must survive atomic replacement of goui.json. Never unlink it.
func lockFrontend(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = retryFrontendLock(path, func() error {
		return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	}, func(err error) bool {
		return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR)
	})
	if err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
