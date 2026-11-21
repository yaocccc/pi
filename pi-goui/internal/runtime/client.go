// Package runtime owns an exclusive native-session writer and Pi's existing
// JSONL protocol without modifying Pi's configuration.
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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Bin, Cwd, AgentDir, SessionFile string
	NoSession                       bool
	Args                            []string
	// ResumeThinking reads session-local metadata under the writer lease, before Pi starts.
	ResumeThinking func(path string) (string, error)
}
type Event struct {
	Data json.RawMessage
	Err  error
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
	lease                *SessionLease
	sessionFile          string
	closing              atomic.Bool
	exitedBeforeClose    atomic.Bool
	closeErr             error
	waitErr              error
	forcedShutdown       atomic.Bool
	writes               chan *writeRequest
	events               chan Event
	eventBatches         chan []Event
	batchNotify          chan struct{}
	batchMode            atomic.Bool
	incoming             chan Event
	done, stop, readDone chan struct{}
	writeDone            chan struct{}
	stderrDone           chan struct{}
	mu                   sync.Mutex
	pending              map[string]chan reply
	extensionPending     map[string]chan reply
	exitErr              error
	stderrTail           []byte
	next                 atomic.Uint64
	closeOnce            sync.Once
	killOnce             sync.Once
	closeDone            chan struct{}
}

func Start(cfg Config) (*Client, error) {
	cfg, err := prepareSessionConfig(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.NoSession {
		return startDirect(cfg)
	}
	lease, err := AcquireSession(cfg.AgentDir, cfg.SessionFile)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(cfg.SessionFile), 0700); err != nil {
		_ = lease.Close()
		return nil, err
	}
	if err = lease.prepareFile(); err != nil {
		_ = lease.abortStart()
		return nil, err
	}
	if cfg.ResumeThinking != nil {
		level, err := cfg.ResumeThinking(cfg.SessionFile)
		if err != nil {
			return nil, errors.Join(err, lease.abortStart())
		}
		if level != "" {
			cfg.Args = append(slices.Clone(cfg.Args), "--thinking", level)
		}
	}
	c, err := startLeased(cfg, lease)
	if err != nil {
		_ = lease.abortStart()
		return nil, err
	}
	return c, nil
}

func startDirect(cfg Config) (*Client, error) { return startLeased(cfg, nil) }

func startLeased(cfg Config, lease *SessionLease) (*Client, error) {
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
	c := &Client{cmd: cmd, lease: lease, sessionFile: cfg.SessionFile, stdin: inW, stdout: outR, stderr: errR, stderrDone: make(chan struct{}), writes: make(chan *writeRequest, 32), events: make(chan Event), eventBatches: make(chan []Event), batchNotify: make(chan struct{}), incoming: make(chan Event), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), writeDone: make(chan struct{}), closeDone: make(chan struct{}), pending: make(map[string]chan reply)}
	if lease != nil {
		if err = lease.attach(cmd); err != nil {
			for _, f := range []*os.File{inR, inW, outR, outW, errR, errW} {
				f.Close()
			}
			return nil, err
		}
	}

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
		if !c.closing.Load() {
			c.exitedBeforeClose.Store(true)
		}
		c.mu.Lock()
		c.waitErr = err
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
	if lease != nil {
		if err := lease.started(cmd.Process.Pid); err != nil {
			c.setFailure(err)
			_ = c.Close()
			return nil, err
		}
	}
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
	if c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}
func (c *Client) SessionFile() string { return c.sessionFile }

// Events and EventBatches consume the same ordered stream. A consumer should
// choose one API, not read both concurrently.
func (c *Client) Events() <-chan Event { return c.events }

// EventBatches delivers up to 64 queued events or 256 KiB without waiting to fill
// a batch. One legal oversized event is delivered alone. Payloads are not copied;
// each returned slice remains valid after subsequent reads. Like Events, closing
// this channel means the stream ended; Err retains its final failure.
func (c *Client) EventBatches() <-chan []Event {
	// Existing single-event consumers need not allocate and copy queue prefixes.
	// Wake a loop already waiting to deliver Events when batching is first used.
	if c.eventBatches != nil && c.batchMode.CompareAndSwap(false, true) && c.batchNotify != nil {
		close(c.batchNotify)
	}
	return c.eventBatches
}

// Err reports why the transport ended. Call it after Events or EventBatches
// closes to recover terminal failures that could not be queued for delivery.
func (c *Client) Err() error {
	select {
	case <-c.readDone:
	case <-c.stop:
	default:
		return nil
	}
	return c.failure()
}

func (c *Client) setFailure(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.exitErr == nil || errors.Is(c.exitErr, io.EOF) {
		c.exitErr = err
	}
}

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

	id := fmt.Sprintf("goui-%d", c.next.Add(1))
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
	if c.lease != nil {
		switch command["type"] {
		case "new_session", "switch_session", "fork":
			return &ResponseError{Command: fmt.Sprint(command["type"]), Message: "close the writer and acquire the target session before changing sessions"}
		}
	}
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
	c.forcedShutdown.Store(true)
	c.setFailure(fmt.Errorf("pi RPC write failed: %w", err))
	c.beginClose()
	c.kill()
}

func (c *Client) readLoop() {
	// Publish the terminal state before eventLoop can close Events.
	defer close(c.incoming)
	defer close(c.readDone)
	defer c.stdout.Close()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), nativeFrameLimit)
	for scanner.Scan() {
		raw := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var envelope struct {
			Type, ID, Command string
			Message           json.RawMessage
			Success           bool
			Fatal             bool
			Data              json.RawMessage
			Error             string
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			if !c.emit(Event{Err: fmt.Errorf("invalid pi JSONL: %w", err)}) {
				return
			}
			continue
		}
		if envelope.Type == "extension_ui_request" && c.extensionReply(raw) {
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
		if !c.emit(Event{Data: append(json.RawMessage(nil), raw...)}) {
			return
		}
	}
	if err := scanner.Err(); err != nil && !c.closing.Load() {
		c.setFailure(fmt.Errorf("read pi stdout: %w", err))
		c.forcedShutdown.Store(true)
		c.kill()
	}
	c.finishProcessRead()
	select {
	case <-c.stop:
	default:
		c.setFailure(errors.New("pi stdout closed"))
		c.emit(Event{Err: c.failure()})
	}
}

// EOF makes the RPC transport unusable, but can precede both Wait and the last
// stderr read. Finish the owned process before publishing its terminal reason,
// without dropping already accepted events or waiting forever on a broken child.
func (c *Client) finishProcessRead() {
	c.stdin.Close()
	select {
	case <-c.done:
	case <-c.stop:
		return
	case <-time.After(2 * time.Second):
		c.setFailure(errors.New("pi stdout closed before process exit"))
		c.forcedShutdown.Store(true)
		c.kill()
		select {
		case <-c.done:
		case <-c.stop:
			return
		case <-time.After(time.Second):
		}
	}
	select {
	case <-c.stderrDone:
	case <-c.stop:
		return
	case <-time.After(time.Second):
		// A descendant outside our process group may still hold stderr open.
		c.stderr.Close()
		<-c.stderrDone
	}
}

func (c *Client) emit(e Event) bool {
	select {
	case c.incoming <- e:
		return true
	case <-c.stop:
		// Close stops presentation, not the backend's final stdout writes.
		// Keep draining until Wait completes; closing the pipe at this point
		// could turn a clean dispose/flush into SIGPIPE and an unclean lease.
		return true
	}
}

// Buffer a bounded local burst, then apply ordinary pipe backpressure. A slow
// view must not kill Pi or discard events. One legal native frame may cross the
// soft byte limit; at most one further frame remains in readLoop.
func (c *Client) eventLoop() {
	defer close(c.events)
	if c.eventBatches != nil {
		defer close(c.eventBatches)
	}
	in := c.incoming
	batchNotify := c.batchNotify
	var queue eventBacklog[Event]
	var batch []Event
	for in != nil || queue.count > 0 {
		var out chan Event
		var batches chan []Event
		var next Event
		if queue.count > 0 {
			out = c.events
			next = queue.first()
			if c.batchMode.Load() {
				if batch == nil {
					batch = queue.batch(eventBatchLimit, eventBatchBytes)
				}
				batches = c.eventBatches
			}
		}
		input := in
		if queue.count >= eventQueueLimit || queue.bytes >= eventQueueBytes {
			input = nil
		}
		select {
		case <-c.stop:
			return
		case <-batchNotify:
			batchNotify = nil
		case e, ok := <-input:
			if !ok {
				in = nil
			} else {
				payloadBytes := len(e.Data)
				if e.Err != nil {
					payloadBytes += len(e.Err.Error())
				}
				queue.append(e, payloadBytes)
			}
		case out <- next:
			queue.remove()
			batch = nil
		case batches <- batch:
			for range batch {
				queue.remove()
			}
			batch = nil
		}
	}
}
func (c *Client) stderrLoop() {
	defer close(c.stderrDone)
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
		if c.cmd != nil {
			killProcessGroup(c.cmd)
		}
	})
}

// Close waits for actual process exit and descendant cleanup. Incomplete or
// unexpected shutdown leaves durable uncertain ownership, never a false idle.
func (c *Client) Close() error {
	c.beginClose()
	<-c.closeDone
	return c.closeErr
}

func (c *Client) beginClose() {
	c.closeOnce.Do(func() {
		c.closing.Store(true)
		close(c.stop)
		go func() {
			defer close(c.closeDone)
			children, cleanupErr := captureDescendants(c.PID())
			_ = c.stdin.Close()
			exited := false
			select {
			case <-c.done:
				exited = true
			case <-time.After(2 * time.Second):
				c.forcedShutdown.Store(true)
				c.kill()
				select {
				case <-c.done:
					exited = true
				case <-time.After(time.Second):
				}
			}
			c.kill()
			cleanupErr = errors.Join(cleanupErr, finishDescendants(children))
			_ = c.stdout.Close()
			if c.stderr != nil {
				_ = c.stderr.Close()
			}
			<-c.writeDone
			if !exited {
				cleanupErr = errors.Join(cleanupErr, errors.New("Pi did not exit after shutdown"))
			}
			if c.lease != nil {
				c.mu.Lock()
				waitErr := c.waitErr
				c.mu.Unlock()
				if waitErr != nil {
					cleanupErr = errors.Join(cleanupErr, waitErr)
				}
				if c.forcedShutdown.Load() {
					cleanupErr = errors.Join(cleanupErr, errors.New("Pi cleanup was forced; session writes may be incomplete"))
				}
				if c.exitedBeforeClose.Load() {
					cleanupErr = errors.Join(cleanupErr, errors.New("Pi launcher exited before controlled shutdown; detached writers cannot be excluded"))
				}
				reason := ""
				if cleanupErr != nil {
					reason = cleanupErr.Error()
				}
				cleanupErr = errors.Join(cleanupErr, c.lease.finish(cleanupErr == nil, reason))
			}
			c.closeErr = cleanupErr
		}()
	})
}
