//go:build (!unix && !windows) || aix

package config

func lockFrontend(path string) (func() error, error) {
	lock, err := acquireSettingsLock(path + ".d")
	if err != nil {
		return nil, err
	}
	return lock.release, nil
}
