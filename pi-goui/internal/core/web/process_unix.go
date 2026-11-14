//go:build unix

package web

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type processIdentity struct {
	pid, parent, group int
	started            string
}

type processTable map[int]processIdentity

// ps is used instead of /proc so the same supervision works on BSD/macOS.
// Explicit system paths and a fixed locale avoid project PATH overrides and
// locale-dependent start times. CommandContext and WaitDelay bound both the
// process and pipe wait; no background scanner survives Server.Close.
func readProcessTable() (processTable, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-A", "-o", "pid=,ppid=,pgid=,lstart=")
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
		if len(fields) != 8 {
			return nil, fmt.Errorf("invalid ps process row: %q", line)
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		group, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil || pid <= 0 || parent < 0 || group < 0 {
			return nil, fmt.Errorf("invalid ps process IDs: %q", line)
		}
		processes[pid] = processIdentity{pid, parent, group, strings.Join(fields[3:], " ")}
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

// Capture before SIGTERM: once the TUI exits, its independently grouped RPC
// children may have been reparented and are no longer discoverable from it.
// The returned cleanup also discovers new descendants while known parents live.
func preparePTYCleanup(cmd *exec.Cmd) func() {
	noop := func() {}
	if cmd.Process == nil {
		return noop
	}
	before, err := readProcessTable()
	if err != nil {
		log.Printf("webui: %v; only the TUI process group can be reclaimed", err)
		return noop
	}
	root, ok := before[cmd.Process.Pid]
	if !ok {
		return noop // Already reaped; never signal a subsequently reused root PID.
	}
	known := processTable{root.pid: root}
	known.collect(before)
	return func() {
		// At the forced deadline stop the still-live root before the final
		// snapshot, so it cannot start another RPC between enumeration and
		// killPTY. After graceful exit os.Process rejects this signal.
		_ = cmd.Process.Signal(syscall.SIGSTOP)
		current, err := readProcessTable()
		if err != nil {
			// Do not blindly signal stale PIDs if identity verification failed.
			log.Printf("webui: %v; descendant cleanup could not be verified", err)
			return
		}
		known.collect(current)
		for pid, previous := range known {
			now, exists := current[pid]
			if pid == root.pid || !exists || now.started != previous.started {
				continue
			}
			// Only a known descendant group leader grants group ownership. A
			// child sharing an unrelated group is terminated individually.
			if now.group == pid {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}
