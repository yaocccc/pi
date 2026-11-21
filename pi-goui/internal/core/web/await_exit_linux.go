//go:build linux

package web

import "golang.org/x/sys/unix"

// Leave the session leader waitable until its session members are captured.
// A zombie pins its PID, so a new PTY cannot have inherited this session ID.
func awaitPTYExit(pid int) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if err != unix.EINTR {
			return err
		}
	}
}
