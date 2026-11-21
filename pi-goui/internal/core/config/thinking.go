package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileSettingsStorage uses proper-lockfile.lockSync(path, {realpath:false}):
// mkdir(path+".lock") excludes both native Pi and other TUI writers.
type settingsLock struct {
	path string
	info os.FileInfo
}

func acquireSettingsLock(path string) (*settingsLock, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := os.Mkdir(path, 0700)
		if err == nil {
			info, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			return &settingsLock{path, info}, nil
		}
		if !errors.Is(err, os.ErrExist) || time.Now().After(deadline) {
			return nil, fmt.Errorf("acquiring settings lock: %w", err)
		}
		// Never remove another writer's lock, even if it appears stale.
		time.Sleep(20 * time.Millisecond)
	}
}

func (l *settingsLock) check() error {
	info, err := os.Lstat(l.path)
	if err != nil {
		return err
	}
	if !os.SameFile(l.info, info) {
		return fmt.Errorf("settings lock replaced: %s", l.path)
	}
	return nil
}

func (l *settingsLock) release() error {
	if err := l.check(); err != nil {
		return err
	}
	return os.Remove(l.path)
}

func SaveDefaultThinkingLevel(dir, level string) (err error) {
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "settings.json")
	lock, err := acquireSettingsLock(path + ".lock")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.release()) }()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	settings := map[string]json.RawMessage{}
	if len(data) != 0 {
		if err = json.Unmarshal(data, &settings); err != nil {
			return err
		}
		if settings == nil {
			return fmt.Errorf("%s: expected settings object", path)
		}
	}
	settings["defaultThinkingLevel"], err = json.Marshal(level)
	if err != nil {
		return err
	}
	data, err = json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if stat, e := os.Stat(path); e == nil {
		err = f.Chmod(stat.Mode().Perm())
	}
	if err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// The SDK can reclaim a lock whose mtime is 10s old. If a slow sync
	// consumed our lease, fail without replacing the settings snapshot.
	if time.Since(lock.info.ModTime()) >= 9*time.Second {
		return fmt.Errorf("settings lock expired: %s", lock.path)
	}
	if err = lock.check(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
