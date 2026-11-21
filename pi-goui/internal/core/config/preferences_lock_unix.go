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
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
