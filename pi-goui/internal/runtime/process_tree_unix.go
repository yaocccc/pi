//go:build unix

package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func processLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		return false
	} else if err != nil && err != syscall.EPERM {
		return true // An inspection failure is not evidence of process exit.
	}
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if end := strings.LastIndex(string(raw), ") "); end >= 0 && strings.HasPrefix(string(raw)[end+2:], "Z ") {
			return false
		}
	}
	return true
}
func processBirth(pid int) string {
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if end := strings.LastIndex(string(raw), ") "); end >= 0 {
			fields := strings.Fields(string(raw)[end+2:])
			if len(fields) > 19 {
				boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
				return "proc:" + strings.TrimSpace(string(boot)) + ":" + fields[19]
			}
		}
	}
	raw, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
func sameProcess(pid int, birth string) bool {
	if !processLive(pid) {
		return false
	}
	current := processBirth(pid)
	return birth == "" || current == "" || current == birth
}

// Unlike a cleanup target, a recovery candidate must be proved gone. A failed
// identity lookup (including switching from /proc to ps) cannot prove PID reuse.
func sessionProcessGone(pid int, birth string) bool {
	if pid <= 0 || birth == "" || strings.HasPrefix(birth, "proc::") {
		return false
	}
	if !processLive(pid) {
		return true
	}
	current := processBirth(pid)
	if current == "" || strings.HasPrefix(current, "proc::") || strings.HasPrefix(current, "proc:") != strings.HasPrefix(birth, "proc:") {
		return false
	}
	return current != birth
}

// A launcher that dropped its descriptor may leave group members behind after
// reparenting. Zombies cannot write, but any live member (including a stopped
// one) or failed inspection keeps recovery closed. Deliberately detached
// processes with neither this group nor the inherited lock are not provable.
func sessionProcessGroupGone(pgid int, birth string) error {
	if pgid <= 0 {
		return fmt.Errorf("previous writer process group is unknown")
	}
	// Linux keeps a process-group ID reserved while any old member remains.
	// A proven new incarnation of its leader therefore belongs to a new group,
	// not to the dead writer. Never mistake that unrelated process for a writer.
	current := processBirth(pgid)
	if strings.HasPrefix(birth, "proc:") && strings.HasPrefix(current, "proc:") && !strings.HasPrefix(current, "proc::") && current != birth {
		return nil
	}
	if err := syscall.Kill(-pgid, 0); err == syscall.ESRCH {
		return nil
	} else if err != nil && err != syscall.EPERM {
		return fmt.Errorf("inspect previous writer process group: %w", err)
	}
	// Include threads: a zombie group leader alone does not prove that its
	// remaining threads have stopped writing or released their descriptors.
	raw, err := exec.Command("/bin/ps", "-eLo", "pid=,pgid=,stat=").Output()
	if err != nil {
		return fmt.Errorf("inspect previous writer process group: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return fmt.Errorf("cannot verify previous writer process group: malformed process table")
		}
		pid, e1 := strconv.Atoi(fields[0])
		group, e2 := strconv.Atoi(fields[1])
		if e1 != nil || e2 != nil || pid <= 0 || group < 0 {
			return fmt.Errorf("cannot verify previous writer process group: invalid process identity")
		}
		if group == pgid && !strings.HasPrefix(fields[2], "Z") {
			return fmt.Errorf("previous writer process group still contains live process %d", pid)
		}
	}
	return nil
}

// Capture before closing stdin: a wrapper may otherwise exit and reparent its
// Pi/tools. PID birth identities keep cleanup from targeting recycled PIDs.
func captureDescendants(pid int) (map[int]string, error) {
	raw, err := exec.Command("/bin/ps", "-eo", "pid=,ppid=").Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Pi descendants: %w", err)
	}
	parents := map[int]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		var p, pp int
		if _, e := fmt.Sscan(line, &p, &pp); e == nil {
			parents[p] = pp
		}
	}
	out := map[int]string{}
	for child := range parents {
		seen := map[int]bool{}
		for p := child; p > 1 && !seen[p]; p = parents[p] {
			seen[p] = true
			if parents[p] == pid {
				out[child] = processBirth(child)
				break
			}
		}
	}
	return out, nil
}
func finishDescendants(children map[int]string) error {
	for pid, birth := range children {
		if sameProcess(pid, birth) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	deadline := time.Now().Add(time.Second)
	for {
		alive := 0
		for pid, birth := range children {
			if sameProcess(pid, birth) {
				alive = pid
				break
			}
		}
		if alive == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Pi descendant %d did not exit", alive)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
