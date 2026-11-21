//go:build unix

package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type processIdentity struct {
	pid, parent, group int
	started            string
	zombie             bool
}

type processTable map[int]processIdentity

// ps is used instead of /proc so the same supervision works on BSD/macOS.
// Explicit system paths and a fixed locale avoid project PATH overrides and
// locale-dependent start times. CommandContext and WaitDelay bound both the
// process and pipe wait; no background scanner survives Server.Close.
func readProcessTable() (processTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-A", "-o", "pid=,ppid=,pgid=,stat=,lstart=")
	cmd.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("enumerate TUI descendants: %w", err)
	}
	return parseProcessTable(string(out))
}

func parseProcessTable(out string) (processTable, error) {
	processes := make(processTable)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 9 {
			return nil, fmt.Errorf("invalid ps process row: %q", line)
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		group, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil || pid <= 0 || parent < 0 || group < 0 {
			return nil, fmt.Errorf("invalid ps process IDs: %q", line)
		}
		processes[pid] = processIdentity{pid, parent, group, strings.Join(fields[4:], " "), strings.HasPrefix(fields[3], "Z")}
	}
	return processes, nil
}

func (known processTable) collect(current processTable) {
	// Retained identities remain useful when their parents exit and init
	// adopts them. Do not follow a recycled PID to an unrelated process tree.
	live := make(map[int]bool)
	for pid, previous := range known {
		if now, ok := current[pid]; ok && now.started == previous.started {
			live[pid] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for pid, process := range current {
			if !live[pid] && live[process.parent] {
				known[pid] = process
				live[pid] = true
				changed = true
			}
		}
	}
}

// startPTY creates a fresh session with the TUI as leader. setpgid (used
// by the RPC) and reparenting both preserve that session. Capture while the
// leader is still waitable: its numeric SID cannot have been reused.
func snapshotPTY(pid int) (processTable, error) {
	current, err := readProcessTable()
	if err != nil {
		return nil, err
	}
	root, ok := current[pid]
	if !ok {
		return nil, fmt.Errorf("cannot verify PTY leader %d", pid)
	}
	known, err := sessionMembers(pid, current)
	if err != nil {
		return nil, err
	}
	known[pid] = root
	known.collect(current)
	delete(known, pid)
	return known, nil
}

func sessionMembers(pid int, current processTable) (processTable, error) {
	known := make(processTable)
	for p, row := range current {
		if row.zombie {
			continue
		}
		sid, err := unix.Getsid(p)
		if errors.Is(err, unix.ESRCH) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect PTY session membership of %d: %w", p, err)
		}
		if sid == pid {
			known[p] = row
		}
	}
	return known, nil
}

func (known processTable) reclaim(sid int) error {
	// Acquire handles first, then recheck identities on the next snapshot
	// before signalling. On Linux Go's pidfd also closes the check/kill race.
	handles := make(map[int]*os.Process)
	defer func() {
		for _, p := range handles {
			_ = p.Release()
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		current, err := readProcessTable()
		if err != nil {
			return err
		}
		// A surviving captured member pins the session ID across snapshots.
		// Without that witness, do not attach a possibly recycled session.
		members, err := sessionMembers(sid, current)
		if err != nil {
			return err
		}
		if len(members) > 0 {
			verified := false
			for pid, row := range members {
				if old, ok := known[pid]; ok && old.started == row.started {
					verified = true
					break
				}
			}
			if !verified {
				return fmt.Errorf("cannot verify remaining PTY session %d members", sid)
			}
			for pid, row := range members {
				known[pid] = row
			}
		}
		known.collect(current)
		for pid, previous := range known {
			now, ok := current[pid]
			if !ok || now.started != previous.started || now.zombie {
				delete(known, pid)
				continue
			}
			p := handles[pid]
			if p == nil {
				p, err = os.FindProcess(pid)
				if err != nil {
					return fmt.Errorf("open PTY descendant %d: %w", pid, err)
				}
				handles[pid] = p
				continue
			}
			if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, unix.ESRCH) {
				return fmt.Errorf("reclaim PTY descendant %d: %w", pid, err)
			}
		}
		if len(known) == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("could not verify termination of %d PTY descendants", len(known))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}
