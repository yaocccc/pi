//go:build unix && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package web

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Where neither waitid(WNOWAIT) nor kqueue is available, inspect only the
// child, without reaping it or periodically scanning the whole process tree.
func awaitPTYExit(pid int) error {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		cmd := exec.CommandContext(ctx, "/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid))
		cmd.Env = []string{"LC_ALL=C", "PATH=/usr/bin:/bin"}
		cmd.WaitDelay = 100 * time.Millisecond
		out, err := cmd.Output()
		cancel()
		if err != nil {
			return fmt.Errorf("inspect PTY leader %d: %w", pid, err)
		}
		if bytes.HasPrefix(bytes.TrimSpace(out), []byte("Z")) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}
