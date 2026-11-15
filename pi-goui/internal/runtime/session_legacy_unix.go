//go:build unix

package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// This is migration protection only. It cannot attach to or launch an old
// broker. Old registries are read-only, and uncertain startup records fail
// closed even when their original TUI has died.
func probeLegacySessions(agent, session string) (SessionStatus, error) {
	if _, err := os.Lstat(filepath.Join(agent, "goui-rpc")); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return SessionStatus{}, err
		}
		return SessionStatus{Busy: true, Uncertain: true, SessionFile: session, Reason: "legacy agent/goui-rpc directory exists; stop old instances and handle the old directory manually"}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return SessionStatus{}, err
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return SessionStatus{}, err
	}
	if err = checkedSessionDir(home, false, false); err != nil {
		return SessionStatus{}, err
	}
	if err = checkedSessionDir(filepath.Join(home, ".pi"), false, false); errors.Is(err, os.ErrNotExist) {
		return SessionStatus{}, nil
	} else if err != nil {
		return SessionStatus{}, err
	}
	base := filepath.Join(home, ".pi", "goui-rpc")
	if err = checkedSessionDir(base, false, true); errors.Is(err, os.ErrNotExist) {
		return SessionStatus{}, nil
	} else if err != nil {
		return SessionStatus{}, err
	}
	// All agent domains are inspected because two configurations can name the
	// same file. A live old coordinator could still start a writer, so mixed
	// old/new execution is rejected until those old TUIs have stopped.
	namespaces, err := os.ReadDir(base)
	if err != nil {
		return SessionStatus{}, err
	}
	for _, ns := range namespaces {
		root := filepath.Join(base, ns.Name())
		if err = checkedSessionDir(root, false, true); err != nil {
			return SessionStatus{}, err
		}
		paths := []string{root, filepath.Join(root, "coordinator")}
		executors, e := os.ReadDir(filepath.Join(root, "executors"))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return SessionStatus{}, e
		}
		for _, executor := range executors {
			paths = append(paths, filepath.Join(root, "executors", executor.Name()))
		}
		for _, path := range paths {
			if e = checkedSessionDir(path, false, true); errors.Is(e, os.ErrNotExist) {
				continue
			} else if e != nil {
				return SessionStatus{}, e
			}
			lock := filepath.Join(path, "owner", "lock")
			if filepath.Base(path) == "coordinator" {
				lock = filepath.Join(path, "lock")
			} else if e = checkedSessionDir(filepath.Join(path, "owner"), false, true); e != nil && !errors.Is(e, os.ErrNotExist) {
				return SessionStatus{}, e
			}
			f, e := lockSessionFile(lock, false)
			if lockContended(e) {
				return SessionStatus{Busy: true, Uncertain: true, SessionFile: session, Reason: "old shared runtime is active; stop its TUI and Pi processes before writing"}, nil
			}
			if e == nil {
				f.Close()
			} else if !errors.Is(e, os.ErrNotExist) {
				return SessionStatus{}, e
			}
			var record struct {
				PID          int
				Backends     []int
				Birth        string
				BackendBirth map[int]string
				Starting     bool
			}
			e = readSessionJSON(filepath.Join(path, "endpoint.json"), &record)
			if errors.Is(e, os.ErrNotExist) {
				continue
			}
			if e != nil {
				return SessionStatus{}, fmt.Errorf("legacy ownership record: %w", e)
			}
			if record.PID <= 0 {
				return SessionStatus{}, errors.New("invalid legacy ownership record")
			}
			busy := record.Starting || sameProcess(record.PID, record.Birth)
			backend := 0
			for _, pid := range record.Backends {
				if sameProcess(pid, record.BackendBirth[pid]) {
					busy = true
					backend = pid
				}
			}
			if busy {
				return SessionStatus{Busy: true, Uncertain: true, OwnerPID: record.PID, BackendPID: backend, SessionFile: session, Reason: "legacy writer is live or its startup is unconfirmed"}, nil
			}
		}
	}
	return SessionStatus{}, nil
}
