package runtime

import (
	"errors"
	"path/filepath"
)

// CanonicalAgentDir normalizes an existing agent identity without creating it.
func CanonicalAgentDir(agent string) (string, error) {
	if agent == "" {
		return "", errors.New("agent directory is required")
	}
	abs, err := filepath.Abs(agent)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}
