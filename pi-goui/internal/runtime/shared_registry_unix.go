//go:build unix

package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Legacy registries are never migrated, removed or opened for writing. Even an
// empty one can belong to an old process still starting; do not guess ownership.
func checkLegacySharedRoot(agent string) error {
	path := filepath.Join(agent, "goui-rpc")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot check legacy shared RPC registry %q: %w", path, err)
	}
	return fmt.Errorf("legacy shared RPC registry %q exists; stop old instances and their Pi backends, then handle the old directory manually before retrying; refusing a duplicate writer", path)
}

func sharedAgentHash(agent string) string {
	hash := sha256.Sum256([]byte(agent))
	return hex.EncodeToString(hash[:])
}

// HOME aliases resolve to the same namespace. The managed .pi/registry paths
// themselves must not be symlinks; no existing permissions are changed.
func sharedRegistryBase(create bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", errors.New("shared RPC registry requires an absolute home directory")
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return "", fmt.Errorf("shared RPC home: %w", err)
	}
	if err = checkedSharedParent(home); err != nil {
		return "", err
	}
	parent := filepath.Join(home, ".pi")
	if create {
		if err = os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	if err = checkedSharedParent(parent); err != nil {
		return "", err
	}
	base := filepath.Join(parent, "goui-rpc")
	if err = checkedSharedDirectory(base, create); err != nil {
		return "", err
	}
	return base, nil
}

func checkedSharedParent(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("shared RPC parent %q must be an owned directory without group/world write access or symlinks", path)
	}
	return nil
}

func checkedSharedDirectory(path string, create bool) error {
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !ownedPrivate(info, true) {
		return fmt.Errorf("shared RPC directory %q must be private and owned, without symlinks", path)
	}
	return nil
}

// A helper may serve only an executor in this user's canonical registry. The
// specific agent identity is subsequently checked on every attach, before any
// backend is created or a caller-supplied environment is installed.
func validateExecutorRegistry(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("invalid session executor registry path")
	}
	base, err := sharedRegistryBase(false)
	if err != nil {
		return err
	}
	executors, namespace := filepath.Dir(root), filepath.Dir(filepath.Dir(root))
	if filepath.Dir(namespace) != base || filepath.Base(executors) != "executors" {
		return errors.New("invalid session executor registry path")
	}
	for _, path := range []string{namespace, executors, root} {
		if err := checkedSharedDirectory(path, false); err != nil {
			return err
		}
	}
	for _, id := range []string{filepath.Base(namespace), filepath.Base(root)} {
		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != id {
			return errors.New("invalid session executor registry name")
		}
	}
	return nil
}

func executorAgent(root, agent string) (string, error) {
	canonical, err := CanonicalAgentDir(agent)
	if err != nil {
		return "", err
	}
	if err = checkLegacySharedRoot(canonical); err != nil {
		return "", err
	}
	if err = validateExecutorRegistry(root); err != nil {
		return "", err
	}
	if filepath.Base(filepath.Dir(filepath.Dir(root))) != sharedAgentHash(canonical) {
		return "", errors.New("shared RPC executor belongs to another agent directory")
	}
	return canonical, nil
}
