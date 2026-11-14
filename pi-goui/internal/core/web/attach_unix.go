//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/coder/websocket"
	"golang.org/x/sys/unix"
)

// Reset the display modes the hosted TUI can leave enabled on detachment. The
// desktop is a detachable display, so it cannot rely on the still-running TUI
// to perform its normal terminal cleanup at this point.
const attachReset = "\x1b[?2026l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1004l\x1b[?1006l\x1b[?1016l\x1b[?2004l\x1b[<u\x1b[>4;0m\x1b[0m\x1b[?25h\x1b[?1049l"

var errLocalDetach = errors.New("desktop detached")

func checkAttachTerminal(input, output *os.File) error {
	if input == nil || output == nil || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return errTerminalRequired
	}
	return nil
}

// Nonblocking syscalls plus short polls make both input and a flow-controlled
// output cancellable without abandoning blocked goroutines. Original file
// status flags are restored after all readers/writers have joined.
type attachFD struct {
	fd, flags int
}

func openAttachFD(file *os.File) (attachFD, error) {
	fd := int(file.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return attachFD{}, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		return attachFD{}, err
	}
	return attachFD{fd, flags}, nil
}
func (f attachFD) restore() error {
	_, err := unix.FcntlInt(uintptr(f.fd), unix.F_SETFL, f.flags)
	return err
}
func (f attachFD) ready(ctx context.Context, events int16) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fds := []unix.PollFd{{Fd: int32(f.fd), Events: events}}
		n, err := unix.Poll(fds, 50)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 {
			if fds[0].Revents&unix.POLLNVAL != 0 {
				return os.ErrClosed
			}
			return nil // HUP/ERR are resolved by the subsequent read/write.
		}
	}
}
func (f attachFD) read(ctx context.Context, data []byte) (int, error) {
	for {
		if err := f.ready(ctx, unix.POLLIN); err != nil {
			return 0, err
		}
		n, err := unix.Read(f.fd, data)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err == nil && n == 0 {
			err = io.EOF
		}
		return n, err
	}
}
func (f attachFD) write(ctx context.Context, data []byte) error {
	for len(data) > 0 {
		if err := f.ready(ctx, unix.POLLOUT); err != nil {
			return err
		}
		n, err := unix.Write(f.fd, data)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func attachSize(fd uintptr) (int, int) {
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 || rows <= 0 {
		return 80, 24
	}
	return max(20, min(500, cols)), max(5, min(200, rows))
}

func attach(parent context.Context, opts AttachOptions) (result AttachResult, err error) {
	if err = checkAttachTerminal(opts.Input, opts.Output); err != nil {
		return result, err
	}
	if opts.ProjectID == "" {
		return result, errors.New("select a project before attaching")
	}
	u, err := url.Parse(opts.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return result, errors.New("invalid shared host URL")
	}
	origin := u.Scheme + "://" + u.Host
	u.Path, u.RawQuery, u.Fragment = "/api/terminal", "", ""
	cols, rows := attachSize(opts.Output.Fd())
	query := url.Values{"cols": {strconv.Itoa(cols)}, "rows": {strconv.Itoa(rows)}, "projectId": {opts.ProjectID}}
	u.RawQuery = query.Encode()
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	client := opts.HTTPClient
	if client == nil {
		transport := &http.Transport{Proxy: nil}
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport}
	}
	conn, _, err := websocket.Dial(dialCtx, u.String(), &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + opts.Token}, "Origin": {origin}},
		CompressionMode: websocket.CompressionDisabled,
	})
	dialCancel()
	if err != nil {
		return result, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(8 << 20)
	inputFD, outputFD := opts.Input.Fd(), opts.Output.Fd()
	state, err := term.MakeRaw(inputFD)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, term.Restore(inputFD, state)) }()
	input, err := openAttachFD(opts.Input)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, input.restore()) }()
	output, err := openAttachFD(opts.Output)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, output.restore()) }()
	defer func() {
		resetCtx, resetCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer resetCancel()
		_ = output.write(resetCtx, []byte(attachReset+ansi.SetPointerShape("default")))
	}()
	var wg sync.WaitGroup
	// Run this before reset/raw restoration, so no old reader can steal input
	// from the detached prompt or race a new attach instance.
	defer func() { cancel(); _ = conn.CloseNow(); wg.Wait() }()
	failures := make(chan error, 2)
	fail := func(e error) {
		select {
		case failures <- e:
		default:
		}
		cancel()
	}
	write := func(kind websocket.MessageType, data []byte) error {
		writeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		return conn.Write(writeCtx, kind, data)
	}
	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)
	started := false
	for {
		kind, data, readErr := conn.Read(ctx)
		if readErr != nil {
			select {
			case e := <-failures:
				if errors.Is(e, errLocalDetach) {
					return AttachResult{Type: "detached"}, nil
				}
				return result, e
			default:
			}
			if parent.Err() != nil {
				return result, parent.Err()
			}
			return result, readErr
		}
		if kind == websocket.MessageBinary {
			outCtx, done := context.WithTimeout(ctx, 5*time.Second)
			err = output.write(outCtx, data)
			done()
			if err != nil {
				return result, err
			}
			continue
		}
		var message AttachResult
		if err = json.Unmarshal(data, &message); err != nil {
			return result, fmt.Errorf("shared terminal control: %w", err)
		}
		switch message.Type {
		case "ready":
			if started {
				continue
			}
			started = true
			wg.Add(2)
			go func() {
				defer wg.Done()
				buf := make([]byte, 16<<10)
				var activityInput inputActivity
				for {
					readCtx, cancelRead := context.WithTimeout(ctx, 60*time.Millisecond)
					n, e := input.read(readCtx, buf)
					cancelRead()
					idle := errors.Is(e, context.DeadlineExceeded) && ctx.Err() == nil
					active := false
					if idle {
						active = activityInput.idle()
					} else if e == nil {
						active = activityInput.feed(buf[:n])
					}
					if active {
						c, r := attachSize(outputFD)
						activity, _ := json.Marshal(map[string]any{"type": "activity", "cols": c, "rows": r})
						e = write(websocket.MessageText, activity)
						if e != nil {
							fail(e)
							return
						}
					}
					if idle {
						continue
					}
					if e == nil {
						if cut := bytes.IndexByte(buf[:n], 0x1d); cut >= 0 { // Ctrl+] detaches locally.
							if cut > 0 {
								e = write(websocket.MessageBinary, buf[:cut])
							}
							if e == nil {
								e = errLocalDetach
							}
						} else {
							e = write(websocket.MessageBinary, buf[:n])
						}
					}
					if e != nil {
						fail(e)
						return
					}
				}
			}()
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					case <-resizes:
						c, r := attachSize(outputFD)
						data, _ := json.Marshal(map[string]any{"type": "resize", "cols": c, "rows": r})
						if e := write(websocket.MessageText, data); e != nil {
							fail(e)
							return
						}
					}
				}
			}()
		case "taken_over", "exit", "restart", "project_closed", "project_changed":
			return message, nil
		}
	}
}

func waitForRetake(ctx context.Context, file *os.File) (ok bool, err error) {
	if file == nil || !term.IsTerminal(file.Fd()) {
		return false, errTerminalRequired
	}
	fd, err := openAttachFD(file)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, fd.restore()) }()
	var buf [256]byte
	for {
		n, err := fd.read(ctx, buf[:])
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		for _, b := range buf[:n] {
			if b == '\n' || b == '\r' {
				return true, nil
			}
		}
	}
}
