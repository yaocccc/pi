package config

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func lockFrontend(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = retryFrontendLock(path, func() error {
		return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	}, func(err error) bool {
		return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
	})
	if err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
