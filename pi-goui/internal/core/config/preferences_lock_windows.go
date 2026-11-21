package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFrontend(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
	if err != nil {
		f.Close()
		return nil, err
	}
	return f.Close, nil
}
