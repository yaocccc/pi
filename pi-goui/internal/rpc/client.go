// Package rpc implements the existing pi JSONL protocol without modifying pi's configuration.
package rpc

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
	ctx    context.Context
	data   []byte
	result chan error
}
type Client struct {
	cmd                  *exec.Cmd
	stdin                *os.File
	stdout, stderr       *os.File
	writes               chan writeRequest
	events               chan Event
	incoming             chan Event
	done, stop, readDone chan struct{}
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
	c := &Client{cmd: cmd, stdin: inW, stdout: outR, stderr: errR, writes: make(chan writeRequest, 32), events: make(chan Event), incoming: make(chan Event, 32), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), closeDone: make(chan struct{}), pending: make(map[string]chan reply)}
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
		c.exitErr = err
		c.mu.Unlock()
		close(c.done)
		c.stdin.Close()
		c.kill()
	}()
	return c, nil
}

// Runtime-local terminal/session markers must not make a child attach to its parent.
func childEnvironment(agentDir string) []string {
	env := []string{"PI_GOUI_PROTOCOL=1", "PI_GOUI_ASK_QUESTION=1"}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "PI_WORKBENCH_") || strings.HasPrefix(key, "PI_SURFACE_") || strings.HasPrefix(key, "CMUX_") || strings.HasPrefix(key, "HERDR_") || strings.HasPrefix(key, "KITTY_") || strings.HasPrefix(key, "WEZTERM_") {
			continue
		}
		switch key {
		case "TMUX", "TMUX_PANE", "STY", "WINDOWID", "PI_SESSION_ID", "PI_SESSION_FILE", "PI_GOUI_ASK_QUESTION", "PI_GOUI_PROTOCOL":
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
func (c *Client) PID() int             { return c.cmd.Process.Pid }
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
	data, err := json.Marshal(command)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	r := writeRequest{ctx, data, make(chan error, 1)}
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
	for {
		select {
		case <-c.done:
			return
		case <-c.stop:
			return
		case r := <-c.writes:
			if err := r.ctx.Err(); err != nil {
				r.result <- err
				continue
			}
			_, err := c.stdin.Write(r.data)
			r.result <- err
		}
	}
}
func (c *Client) readLoop() {
	defer close(c.readDone)
	defer close(c.incoming)
	defer c.stdout.Close()
	scanner := bufio.NewScanner(c.stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		raw := bytes.TrimSuffix(scanner.Bytes(), []byte{'\r'})
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var envelope struct {
			Type, ID, Command string
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
		if c.extensionReply(raw) {
			continue
		}
		if envelope.Type == "response" && envelope.ID != "" {
			c.mu.Lock()
			ch := c.pending[envelope.ID]
			c.mu.Unlock()
			if ch != nil {
				r := reply{data: envelope.Data}
				if !envelope.Success {
					r.err = fmt.Errorf("pi %s: %s", envelope.Command, envelope.Error)
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

func (c *Client) kill() { c.killOnce.Do(func() { killProcessGroup(c.cmd) }) }

// Close is concurrent-safe, closes stdin for graceful disposal, then kills the
// isolated process group after two seconds. Cmd.Wait has exactly one owner.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		defer close(c.closeDone)
		close(c.stop)
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
		c.stderr.Close()
	})
	<-c.closeDone
	return nil
}
