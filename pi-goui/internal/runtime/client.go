// Package runtime coordinates local instances and shared sessions using Pi's
// existing JSONL protocol without modifying Pi's configuration.
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Bin, Cwd, AgentDir, SessionFile string
	NoSession                       bool
	Args                            []string
	relayExtensions                 bool
	environment                     []string
}
type Event struct {
	Data  json.RawMessage
	Err   error
	owner string // Captured at the backend transport edge, before response dispatch.
}
type reply struct {
	data json.RawMessage
	err  error
}
type writeRequest struct {
	ctx     context.Context
	data    []byte
	result  chan error
	mu      sync.Mutex // Serializes completion with the sender's cancellation.
	writing bool
}
type Client struct {
	cmd                  *exec.Cmd
	stdin                io.WriteCloser
	stdout               io.ReadCloser
	stderr               *os.File
	shared               *sharedConnection
	idPrefix             string
	relayExtensions      bool
	writes               chan *writeRequest
	events               chan Event
	incoming             chan Event
	done, stop, readDone chan struct{}
	writeDone            chan struct{}
	mu                   sync.Mutex
	pending              map[string]chan reply
	extensionPending     map[string]chan reply
	exitErr              error
	stderrTail           []byte
	next                 atomic.Uint64
	eventOwner           atomic.Pointer[string]
	closeOnce            sync.Once
	killOnce             sync.Once
	closeDone            chan struct{}
}

func Start(cfg Config) (*Client, error) {
	if sharingEnabled.Load() && !cfg.NoSession {
		return startShared(cfg)
	}
	return startDirect(cfg)
}

func startDirect(cfg Config) (*Client, error) {
	bin := cfg.Bin
	if bin == "" {
		bin = os.Getenv("PI_BIN")
	}
	if bin == "" {
		bin = "pi"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("pi executable %q: %w", bin, err)
	}
	args := append([]string{}, cfg.Args...)
	// The transport owns mode and session selection, not the caller's pass-through flags.
	args = append(args, "--mode", "rpc")
	if cfg.NoSession {
		args = append(args, "--no-session")
	}
	if cfg.SessionFile != "" {
		args = append(args, "--session", cfg.SessionFile)
	}
	cmd := exec.Command(resolved, args...)
	cmd.Dir = cfg.Cwd
	cmd.Env = childEnvironment(cfg.AgentDir)
	if cfg.environment != nil {
		cmd.Env = append([]string(nil), cfg.environment...)
	}
	configureProcess(cmd)
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdin = inR
	cmd.Stdout = outW
	cmd.Stderr = errW
	c := &Client{cmd: cmd, relayExtensions: cfg.relayExtensions, stdin: inW, stdout: outR, stderr: errR, writes: make(chan *writeRequest, 32), events: make(chan Event), incoming: make(chan Event, 32), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), writeDone: make(chan struct{}), closeDone: make(chan struct{}), pending: make(map[string]chan reply)}
	if err = cmd.Start(); err != nil {
		for _, f := range []*os.File{inR, inW, outR, outW, errR, errW} {
			f.Close()
		}
		return nil, err
	}
	inR.Close()
	outW.Close()
	errW.Close()
	go c.readLoop()
	go c.writeLoop()
	go c.eventLoop()
	go c.stderrLoop()
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		if err == nil {
			err = io.EOF
		}
		if c.exitErr == nil {
			c.exitErr = err
		}
		c.mu.Unlock()
		close(c.done)
		c.stdin.Close()
		c.kill()
	}()
	return c, nil
}

// Runtime-local terminal/session markers must not make a child attach to its parent.
func childEnvironment(agentDir string) []string {
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "PI_WORKBENCH_") || strings.HasPrefix(key, "PI_SURFACE_") || strings.HasPrefix(key, "CMUX_") || strings.HasPrefix(key, "HERDR_") || strings.HasPrefix(key, "KITTY_") || strings.HasPrefix(key, "WEZTERM_") {
			continue
		}
		switch key {
		case "TMUX", "TMUX_PANE", "STY", "WINDOWID", "PI_SESSION_ID", "PI_SESSION_FILE", "PI_GOUI_ASK_QUESTION", "PI_GOUI_PROTOCOL", "PI_UI_PROTOCOL":
			continue
		case "PI_CODING_AGENT_DIR":
			if agentDir != "" {
				continue
			}
		}
		env = append(env, entry)
	}
	if agentDir != "" {
		env = append(env, "PI_CODING_AGENT_DIR="+agentDir)
	}
	return env
}
func (c *Client) PID() int {
	if c.shared != nil {
		return c.shared.pid
	}
	return c.cmd.Process.Pid
}
func (c *Client) Events() <-chan Event { return c.events }
func (c *Client) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.exitErr
	if err == nil {
		err = errors.New("pi RPC closed")
	}
	if len(c.stderrTail) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(c.stderrTail)))
	}
	return err
}

// Call returns response.data, not the outer response record. The caller owns its deadline.
func (c *Client) Call(ctx context.Context, command map[string]any) (json.RawMessage, error) {
	return c.call(ctx, command, nil)
}

// sent releases the executor's transaction only after the complete frame has
// been written. Waiting for Pi's acknowledgement must not block abort or UI.
func (c *Client) call(ctx context.Context, command map[string]any, sent func()) (json.RawMessage, error) {
	id := fmt.Sprintf("goui-%s%d", c.idPrefix, c.next.Add(1))
	request := make(map[string]any, len(command)+1)
	for k, v := range command {
		request[k] = v
	}
	request["id"] = id
	ch := make(chan reply, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.Send(ctx, request); err != nil {
		if ctx.Err() == nil {
			select {
			case <-c.done:
				select {
				case <-c.readDone:
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-c.stop:
					return nil, err
				}
				select {
				case r := <-ch:
					return r.data, r.err
				default:
				}
			default:
			}
		}
		return nil, err
	}
	if sent != nil {
		sent()
	}
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.stop:
		return nil, c.failure()
	case <-c.readDone:
		// A final response can already have been read when the child exits.
		select {
		case r := <-ch:
			return r.data, r.err
		default:
			return nil, c.failure()
		}
	}
}
func (c *Client) Send(ctx context.Context, command map[string]any) error {
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	r := &writeRequest{ctx: ctx, data: data, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stop:
		return c.failure()
	case <-c.done:
		return c.failure()
	case c.writes <- r:
	}
	select {
	case err := <-r.result:
		return err
	case <-ctx.Done():
		r.mu.Lock()
		defer r.mu.Unlock()
		// A completed write wins over later cancellation. A queued request is
		// simply skipped; a started write may have left a partial JSONL record.
		select {
		case err := <-r.result:
			return err
		default:
		}
		if r.writing {
			c.failWrite(ctx.Err())
		}
		return ctx.Err()
	case <-c.stop:
		return c.failure()
	case <-c.done:
		select {
		case err := <-r.result:
			return err
		default:
			return c.failure()
		}
	}
}
func (c *Client) writeLoop() {
	defer close(c.writeDone)
	for {
		select {
		case <-c.done:
			return
		case <-c.stop:
			return
		case r := <-c.writes:
			r.mu.Lock()
			if err := r.ctx.Err(); err != nil {
				r.result <- err
				r.mu.Unlock()
				continue
			}
			select {
			case <-c.stop:
				r.mu.Unlock()
				return
			default:
			}
			r.writing = true
			r.mu.Unlock()

			n, err := c.stdin.Write(r.data)
			r.mu.Lock()
			if cancelled := r.ctx.Err(); cancelled != nil {
				err = cancelled
			} else if err == nil && n != len(r.data) {
				err = io.ErrShortWrite
			}
			if err != nil {
				c.failWrite(err)
			}
			r.writing = false
			r.result <- err
			r.mu.Unlock()
		}
	}
}

// Once a write has started, cancellation/failure makes record framing uncertain.
// Invalidate synchronously, then kill rather than allowing graceful stdin drain
// to deliver a timed-out command. Cleanup never waits on the calling writer.
func (c *Client) failWrite(err error) {
	c.mu.Lock()
	if c.exitErr == nil {
		c.exitErr = fmt.Errorf("pi RPC write failed: %w", err)
	}
	c.mu.Unlock()
	c.beginClose()
	c.kill()
}

func (c *Client) readLoop() {
	defer close(c.readDone)
	defer close(c.incoming)
	defer c.stdout.Close()
	scanner := bufio.NewScanner(c.stdout)
	limit := 64 * 1024 * 1024
	if c.shared != nil {
		// The native backend retains its original limit. A shared frame can
		// additionally carry the executor's small, bounded progress projection.
		limit += workingFrameAllowance
	}
	scanner.Buffer(make([]byte, 64*1024), limit)
	for scanner.Scan() {
		raw := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var envelope struct {
			Type, ID, Command string
			Message           json.RawMessage
			Success           bool
			Data              json.RawMessage
			Error             string
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			if !c.emit(Event{Err: fmt.Errorf("invalid pi JSONL: %w", err)}) {
				return
			}
			continue
		}
		if envelope.Type == "goui_rpc_error" && c.shared != nil {
			var message string
			if err := json.Unmarshal(envelope.Message, &message); err != nil {
				message = "invalid shared RPC error: " + err.Error()
			}
			if !c.emit(Event{Err: errors.New(message)}) {
				return
			}
			continue
		}
		if envelope.Type == "extension_ui_request" && !c.relayExtensions && c.extensionReply(raw) {
			continue
		}
		if envelope.Type == "response" && envelope.ID != "" {
			c.mu.Lock()
			ch := c.pending[envelope.ID]
			c.mu.Unlock()
			if ch != nil {
				r := reply{data: envelope.Data}
				if !envelope.Success {
					r.err = &ResponseError{Command: envelope.Command, Message: envelope.Error}
				}
				select {
				case ch <- r:
				default:
				}
			}
			continue
		}
		owner := ""
		if scope := c.eventOwner.Load(); scope != nil {
			owner = *scope
		}
		if !c.emit(Event{Data: append(json.RawMessage(nil), raw...), owner: owner}) {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		c.emit(Event{Err: fmt.Errorf("read pi stdout: %w", err)})
		go c.Close()
	} else {
		select {
		case <-c.stop:
		default:
			c.emit(Event{Err: fmt.Errorf("pi stdout closed: %w", c.failure())})
		}
	}
}
func (c *Client) emit(e Event) bool {
	select {
	case c.incoming <- e:
		return true
	case <-c.stop:
		return false
	}
}

// Queue independently of the UI so a background session never blocks stdout.
func (c *Client) eventLoop() {
	defer close(c.events)
	in := c.incoming
	var queue []Event
	for in != nil || len(queue) > 0 {
		var out chan Event
		var next Event
		if len(queue) > 0 {
			out = c.events
			next = queue[0]
		}
		select {
		case <-c.stop:
			return
		case e, ok := <-in:
			if !ok {
				in = nil
			} else {
				if c.shared != nil && len(queue) >= 1024 {
					c.mu.Lock()
					c.exitErr = errors.New("shared event backlog exceeded; reconnect and rebuild session state")
					c.mu.Unlock()
					c.beginClose()
					return
				}
				queue = append(queue, e)
			}
		case out <- next:
			queue[0] = Event{}
			queue = queue[1:]
		}
	}
}
func (c *Client) stderrLoop() {
	defer c.stderr.Close()
	buf := make([]byte, 4096)
	for {
		n, err := c.stderr.Read(buf)
		if n > 0 {
			c.mu.Lock()
			c.stderrTail = append(c.stderrTail, buf[:n]...)
			if len(c.stderrTail) > 8192 {
				c.stderrTail = append([]byte(nil), c.stderrTail[len(c.stderrTail)-8192:]...)
			}
			c.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) kill() {
	c.killOnce.Do(func() {
		if c.shared != nil {
			_ = c.stdin.Close()
		} else {
			killProcessGroup(c.cmd)
		}
	})
}

// Close is concurrent-safe, closes stdin for graceful disposal, then kills the
// isolated process group after two seconds. Cmd.Wait has exactly one owner.
func (c *Client) Close() error {
	c.beginClose()
	<-c.closeDone
	return nil
}

func (c *Client) beginClose() {
	c.closeOnce.Do(func() {
		close(c.stop)
		go func() {
			defer close(c.closeDone)
			c.stdin.Close()
			select {
			case <-c.done:
			case <-time.After(2 * time.Second):
				c.kill()
				select {
				case <-c.done:
				case <-time.After(time.Second):
				}
			}
			c.kill()
			c.stdout.Close()
			if c.stderr != nil {
				c.stderr.Close()
			}
			<-c.writeDone
		}()
	})
}
