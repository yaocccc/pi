package runtime

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SessionStatus is advisory. Only AcquireSession authorizes a write; a probe
// must never be used as a check-then-write lock.
type SessionStatus struct {
	Busy, Uncertain                   bool
	OwnerPID, BackendPID, LauncherPID int
	SessionFile, Reason               string
}

var ErrSessionBusy = errors.New("session is owned by another operation")

type SessionBusyError struct{ Status SessionStatus }

func (e *SessionBusyError) Error() string {
	return fmt.Sprintf("session is read-only (owner PID %d, launcher PID %d): %s", e.Status.OwnerPID, e.Status.LauncherPID, e.Status.Reason)
}
func (e *SessionBusyError) Unwrap() error { return ErrSessionBusy }

func sessionAgent(agent string) (string, error) {
	if agent == "" {
		agent = os.Getenv("PI_CODING_AGENT_DIR")
	}
	if agent == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		agent = filepath.Join(home, ".pi", "agent")
	}
	return canonicalMissingPath(agent)
}

// Resolve existing ancestors as well as a not-yet-created native session. The
// leaf itself is checked separately so file operations cannot follow symlinks.
func canonicalMissingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		suffix = append(suffix, filepath.Base(abs))
		abs = parent
	}
}

func sessionPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("session file is required")
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("session file must be a regular file, not a symlink")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return canonicalMissingPath(path)
}

func prepareSessionConfig(cfg Config) (Config, error) {
	// Session selection belongs to the lease, not to pass-through CLI flags.
	for _, arg := range cfg.Args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--session", "--session-dir", "--continue", "-c", "--resume", "-r", "--no-session", "--mode":
			return cfg, fmt.Errorf("runtime owns session selection; pass-through flag %q is not allowed", name)
		}
	}
	if cfg.NoSession {
		cfg.SessionFile = ""
		return cfg, nil
	}
	agent, err := sessionAgent(cfg.AgentDir)
	if err != nil {
		return cfg, err
	}
	cfg.AgentDir = agent
	if cfg.SessionFile == "" {
		cwd := cfg.Cwd
		if cwd == "" {
			cwd, err = os.Getwd()
		}
		if err != nil {
			return cfg, err
		}
		cwd, err = filepath.Abs(cwd)
		if err != nil {
			return cfg, err
		}
		encoded := "--" + strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(strings.TrimLeft(cwd, "/\\")) + "--"
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return cfg, err
		}
		cfg.SessionFile = filepath.Join(agent, "sessions", encoded, strings.ReplaceAll(time.Now().UTC().Format("2006-01-02T15-04-05.000Z"), ".", "-")+fmt.Sprintf("_%x.jsonl", id))
	} else if !filepath.IsAbs(cfg.SessionFile) && cfg.Cwd != "" {
		cfg.SessionFile = filepath.Join(cfg.Cwd, cfg.SessionFile)
	}
	cfg.SessionFile, err = sessionPath(cfg.SessionFile)
	return cfg, err
}
