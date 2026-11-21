//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package web

import "golang.org/x/sys/unix"

func awaitPTYExit(pid int) error {
	q, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(q)
	unix.CloseOnExec(q)
	var change, event [1]unix.Kevent_t
	unix.SetKevent(&change[0], pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ONESHOT)
	change[0].Fflags = unix.NOTE_EXIT
	for {
		_, err = unix.Kevent(q, change[:], event[:], nil)
		if err == unix.EINTR {
			continue
		}
		if err == nil && event[0].Flags&unix.EV_ERROR != 0 {
			err = unix.Errno(event[0].Data)
		}
		if err == unix.ESRCH { // already exited, not yet reaped
			return nil
		}
		return err
	}
}
