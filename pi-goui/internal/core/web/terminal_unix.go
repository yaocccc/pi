//go:build unix

package web

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func startPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	// Do not launch a TUI if this host cannot supervise its independent RPC
	// process groups. The same bounded snapshot is used during shutdown.
	if _, err := readProcessTable(); err != nil {
		return nil, err
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	// Rewrap a nonblocking duplicate so Go's poller can interrupt reads/writes
	// on Close and enforce write deadlines even if pty used File.Fd internally.
	fd, err := unix.Dup(int(f.Fd()))
	if err == nil {
		unix.CloseOnExec(fd)
		err = unix.SetNonblock(fd, true)
	}
	_ = f.Close()
	if err != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		cleanup := preparePTYCleanup(cmd)
		cleanup()
		killPTY(cmd)
		_ = cmd.Wait()
		return nil, err
	}
	return os.NewFile(uintptr(fd), "webui-pty"), nil
}
func resizePTY(f *os.File, cols, rows int) error {
	// SyscallConn avoids File.Fd's switch to blocking mode while other goroutines
	// read/write this PTY. A duplicated descriptor also shares O_NONBLOCK.
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	err = raw.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	})
	if err != nil {
		return err
	}
	return resizeErr
}
func terminatePTY(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}
func killPTY(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	}
}
