package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/coder/websocket"
)

const historyLimit = 4 << 20
const writeTimeout = 5 * time.Second
const heartbeatInterval = 20 * time.Second
const heartbeatTimeout = 10 * time.Second

type frame struct {
	kind websocket.MessageType
	data []byte
	last bool
}
type peer struct {
	conn    *websocket.Conn
	output  chan frame
	stopped chan struct{}
	once    sync.Once
	final   []byte
	// Geometry is protected by terminal.mu after attach.
	cols, rows int
}

func newPeer(c *websocket.Conn) *peer {
	return &peer{conn: c, output: make(chan frame, 64), stopped: make(chan struct{})}
}
func (p *peer) finish(final []byte) { p.once.Do(func() { p.final = final; close(p.stopped) }) }
func (p *peer) send(f frame) {
	select {
	case <-p.stopped:
		return
	default:
	}
	select {
	case p.output <- f:
	default:
		p.finish(nil)
	} // Drop slow connections; the PTY must keep draining.
}

// keepAlive uses WebSocket control frames, not PTY input. Conn.Ping is safe
// alongside the single data writer; connect continuously reads to handle Pong.
func (p *peer) keepAlive(ctx context.Context, interval, timeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopped:
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			err := p.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				p.finish(nil)
				return
			}
		}
	}
}
func (p *peer) writeLoop(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		p.keepAlive(ctx, heartbeatInterval, heartbeatTimeout)
	}()
	defer func() {
		cancel()
		<-heartbeatDone
		_ = p.conn.CloseNow()
	}()
	write := func(kind websocket.MessageType, data []byte) error {
		c, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return p.conn.Write(c, kind, data)
	}
	for {
		select {
		case <-p.stopped:
			if len(p.final) > 0 {
				_ = write(websocket.MessageText, p.final)
			}
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-p.stopped:
			continue
		case f := <-p.output:
			if write(f.kind, f.data) != nil || f.last {
				return
			}
		}
	}
}
func control(kind, message string, code int) []byte {
	raw, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message,omitempty"`
		Code    int    `json:"code,omitempty"`
	}{kind, message, code})
	return raw
}

type historySize struct{ at, cols, rows int }

type terminal struct {
	cmd          *exec.Cmd
	pty          *os.File
	mu           sync.Mutex // history, modes, peers and geometry; never held across PTY writes
	ioMu         sync.Mutex // before mu; serializes input, resize and peer changes
	peers        []*peer    // stable connection order
	sizeOwner    *peer
	cols, rows   int
	history      []byte
	historyStore []byte // allocation base; history is the live sliding window
	historySizes []historySize
	truncated    bool
	modes        map[int]bool
	modifyKeys   int
	keypad       bool
	parser       *ansi.Parser
	exited       bool
	exit         []byte
	done         chan struct{}
	readDone     chan struct{}
	stopOnce     sync.Once
}

func startTerminal(cfg Config, cols, rows int) (*terminal, error) {
	args := cfg.Command
	if cfg.CommandForDirectory != nil {
		args = cfg.CommandForDirectory(cfg.Cwd)
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("TUI command is required")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = cfg.Cwd
	env := cfg.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = terminalEnvironment(env)
	f, err := startPTY(cmd, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("start TUI: %w", err)
	}
	t := &terminal{cmd: cmd, pty: f, cols: cols, rows: rows, done: make(chan struct{}), readDone: make(chan struct{}), modes: make(map[int]bool), parser: ansi.NewParser()}
	t.historySizes = []historySize{{cols: cols, rows: rows}}
	t.trackModes()
	go t.readLoop()
	go t.wait()
	return t, nil
}

func (t *terminal) trackModes() {
	t.parser.SetDataSize(1) // No need to retain OSC/DCS payloads (including clipboard data).
	t.parser.SetHandler(ansi.Handler{HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
		if cmd.Prefix() == '>' && cmd.Final() == 'm' {
			resource, _, _ := params.Param(0, 0)
			level, _, _ := params.Param(1, 0)
			if resource == 4 && level >= 0 && level <= 2 {
				t.modifyKeys = level
			}
		}
		if cmd.Prefix() != '?' || (cmd.Final() != 'h' && cmd.Final() != 'l') {
			return
		}
		params.ForEach(0, func(_, mode int, _ bool) {
			if mode >= 0 && mode <= 2027 {
				t.modes[mode] = cmd.Final() == 'h'
			}
		})
	}, HandleEsc: func(cmd ansi.Cmd) {
		switch cmd.Final() {
		case 'c':
			clear(t.modes)
			t.modifyKeys = 0
			t.keypad = false
		case '=':
			t.keypad = true
		case '>':
			t.keypad = false
		}
	}})
}

func (t *terminal) wait() {
	f, cmd := t.pty, t.cmd
	err := cmd.Wait()
	// The process exited but descendants may still hold the slave open. Drain
	// final output briefly, then close the master instead of leaking a reader.
	select {
	case <-t.readDone:
	case <-time.After(200 * time.Millisecond):
		_ = f.Close()
		<-t.readDone
	}
	_ = f.Close()
	code := 0
	message := "TUI exited"
	if err != nil {
		code = cmd.ProcessState.ExitCode()
		message = err.Error()
	}
	t.mu.Lock()
	t.exited = true
	t.exit = control("exit", message, code)
	for _, p := range t.peers {
		p.send(frame{websocket.MessageText, t.exit, true})
	}
	t.mu.Unlock()
	close(t.done)
}
func terminalEnvironment(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, v := range env {
		if !strings.HasPrefix(v, "TERM=") && !strings.HasPrefix(v, "COLORTERM=") {
			out = append(out, v)
		}
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}
func (t *terminal) readLoop() {
	defer close(t.readDone)
	buf := make([]byte, 16<<10)
	for {
		n, err := t.pty.Read(buf)
		if n > 0 {
			t.record(buf[:n])
		}
		if err != nil {
			return
		}
	}
}
func (t *terminal) record(data []byte) {
	chunk := bytes.Clone(data)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.parser.Parse(chunk)
	t.appendHistory(chunk)
	for _, p := range t.peers {
		p.send(frame{kind: websocket.MessageBinary, data: chunk})
	}
}
func (t *terminal) attach(p *peer) {
	t.ioMu.Lock()
	defer t.ioMu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers = append(t.peers, p)
	t.sizeOwner = p
	// Clients establish the canonical grid before consuming any replay bytes.
	p.send(frame{kind: websocket.MessageText, data: sizeControl("ready", t.cols, t.rows)})
	replay := bytes.Clone(t.history)
	if t.truncated {
		// Cancel an incomplete escape/string before restoring modes. The forced
		// size round trip below repaints the actual TUI after this bounded replay.
		replay = append(replay, []byte("\x18\x1b\\\x1b[0m")...)
		keys := make([]int, 0, len(t.modes))
		for mode := range t.modes {
			keys = append(keys, mode)
		}
		sort.Ints(keys)
		for _, mode := range keys {
			suffix := 'l'
			if t.modes[mode] {
				suffix = 'h'
			}
			replay = fmt.Appendf(replay, "\x1b[?%d%c", mode, suffix)
		}
		replay = append(replay, ansi.ModifyOtherKeys(t.modifyKeys)...)
		if t.keypad {
			replay = append(replay, "\x1b="...)
		} else {
			replay = append(replay, "\x1b>"...)
		}
	}
	// Replay each byte range using the grid on which it was originally drawn.
	// A bounded raw stream alone cannot be replayed at the latest width/height.
	start := 0
	for _, size := range t.historySizes {
		if size.at > start {
			p.send(frame{kind: websocket.MessageBinary, data: replay[start:size.at]})
		}
		p.send(frame{kind: websocket.MessageText, data: sizeControl("size", size.cols, size.rows)})
		start = size.at
	}
	if start < len(replay) {
		p.send(frame{kind: websocket.MessageBinary, data: replay[start:]})
	}
	p.send(frame{kind: websocket.MessageText, data: sizeControl("size", t.cols, t.rows)})
	if t.exited {
		p.send(frame{websocket.MessageText, t.exit, true})
	}
}

func sizeControl(kind string, cols, rows int) []byte {
	data, _ := json.Marshal(struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}{kind, cols, rows})
	return data
}

// chooseSizeOwner requires mu. Stopped/slow peers cannot retain size ownership.
func (t *terminal) chooseSizeOwner() *peer {
	for i := len(t.peers) - 1; i >= 0; i-- {
		p := t.peers[i]
		select {
		case <-p.stopped:
			continue
		default:
		}
		return p
	}
	return nil
}

func (t *terminal) detach(p *peer) {
	t.ioMu.Lock()
	defer t.ioMu.Unlock()
	t.mu.Lock()
	defer t.mu.Unlock()
	for i, candidate := range t.peers {
		if candidate == p {
			t.peers = append(t.peers[:i], t.peers[i+1:]...)
			break
		}
	}
	if t.sizeOwner != p {
		return
	}
	t.sizeOwner = t.chooseSizeOwner()
	if owner := t.sizeOwner; owner != nil && !t.exited {
		_ = t.setSizeLocked(owner.cols, owner.rows)
	}
}
func (t *terminal) owned(p *peer) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exited {
		return false
	}
	select {
	case <-p.stopped:
		return false
	default:
	}
	for _, candidate := range t.peers {
		if candidate == p {
			return true
		}
	}
	return false
}

// activate changes the shared grid only on explicit local user activity.
// Heartbeats, output and terminal query replies never call this method.
func (t *terminal) activate(p *peer, cols, rows int) error {
	t.ioMu.Lock()
	defer t.ioMu.Unlock()
	if !t.owned(p) {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p.cols, p.rows = cols, rows
	for i, candidate := range t.peers {
		if candidate == p {
			t.peers = append(append(t.peers[:i:i], t.peers[i+1:]...), p)
			break
		}
	}
	t.sizeOwner = p
	return t.setSizeLocked(cols, rows)
}

func (t *terminal) input(p *peer, data []byte) error {
	t.ioMu.Lock()
	defer t.ioMu.Unlock()
	if !t.owned(p) {
		return errors.New("connection is no longer attached to this terminal")
	}
	// A wedged application must not block other inputs or shutdown indefinitely.
	if err := t.pty.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return fmt.Errorf("PTY write deadline: %w", err)
	}
	for len(data) > 0 {
		n, err := t.pty.Write(data)
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
func (t *terminal) resize(p *peer, cols, rows int, redraw bool) error {
	t.ioMu.Lock()
	defer t.ioMu.Unlock()
	if !t.owned(p) {
		return nil
	}
	t.mu.Lock()
	p.cols, p.rows = cols, rows
	if redraw {
		t.sizeOwner = p
	}
	if t.sizeOwner != p {
		t.mu.Unlock()
		return nil
	}
	// Repaint after replay even with existing viewers: bounded history can
	// contain output from several grids and is not a terminal-screen snapshot.
	if !redraw {
		err := t.setSizeLocked(cols, rows)
		t.mu.Unlock()
		return err
	}
	t.mu.Unlock()
	if redraw {
		temporary := cols + 1
		if cols == 500 {
			temporary = cols - 1
		}
		t.mu.Lock()
		err := t.setSizeLocked(temporary, rows)
		t.mu.Unlock()
		if err != nil {
			return err
		}
		// Bubble Tea handles SIGWINCH asynchronously; give it a frame to observe
		// the temporary size before restoring the browser's real dimensions.
		select {
		case <-t.done:
			return nil
		case <-p.stopped:
			return nil
		case <-time.After(40 * time.Millisecond):
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.setSizeLocked(cols, rows)
}

// appendHistory requires mu. The live window is at most historyLimit bytes;
// its backing allocation is at most twice that. Trimming advances the window
// without moving its bytes, so copying a full window is amortized over at least
// another window of output instead of being repeated for every PTY read.
func (t *terminal) appendHistory(chunk []byte) {
	if len(chunk) >= historyLimit {
		t.history = bytes.Clone(chunk[len(chunk)-historyLimit:])
		t.historyStore = t.history[:0]
		t.historySizes = []historySize{{cols: t.cols, rows: t.rows}}
		t.truncated = true
		return
	}
	if overflow := len(t.history) + len(chunk) - historyLimit; overflow > 0 {
		t.trimHistory(overflow)
	}
	if needed := len(t.history) + len(chunk); needed > cap(t.history) {
		// Reuse the allocation's discarded prefix when its tail is full.
		// Grow small histories gradually, with at most one spare window.
		store := t.historyStore
		if capacity := min(2*needed, 2*historyLimit); cap(store) < capacity {
			store = make([]byte, 0, capacity)
		}
		history := store[:len(t.history)]
		copy(history, t.history)
		t.history, t.historyStore = history, store[:0]
	}
	t.history = append(t.history, chunk...)
}

// trimHistory requires mu, retaining the geometry at the new replay start.
func (t *terminal) trimHistory(n int) {
	t.history = t.history[n:]
	var sizes []historySize
	for _, size := range t.historySizes {
		size.at -= n
		if size.at <= 0 {
			size.at = 0
			sizes = []historySize{size}
		} else {
			sizes = append(sizes, size)
		}
	}
	t.historySizes = sizes
	t.truncated = true
}

// setSizeLocked holds mu across the nonblocking resize ioctl, ordering the
// size notification before readLoop can publish output rendered at that size.
func (t *terminal) setSizeLocked(cols, rows int) error {
	if !validSize(cols, rows) || (t.cols == cols && t.rows == rows) {
		return nil
	}
	if err := resizePTY(t.pty, cols, rows); err != nil {
		return err
	}
	t.cols, t.rows = cols, rows
	size := historySize{at: len(t.history), cols: cols, rows: rows}
	if n := len(t.historySizes); n > 0 && t.historySizes[n-1].at == size.at {
		t.historySizes[n-1] = size
	} else {
		t.historySizes = append(t.historySizes, size)
	}
	// Bound replay frames as well as bytes, leaving room in each peer queue.
	if len(t.historySizes) > 16 {
		t.trimHistory(t.historySizes[1].at)
	}
	data := sizeControl("size", cols, rows)
	for _, p := range t.peers {
		p.send(frame{kind: websocket.MessageText, data: data})
	}
	return nil
}
func (t *terminal) stop() {
	t.stopOnce.Do(func() {
		t.mu.Lock()
		for _, p := range t.peers {
			p.finish(nil)
		}
		t.mu.Unlock()
		select {
		case <-t.done:
			return
		default:
		}
		cleanup := preparePTYCleanup(t.cmd)
		defer cleanup()
		terminatePTY(t.cmd)
		select {
		case <-t.done:
		case <-time.After(3 * time.Second):
			// Capture/kill independently grouped descendants before the root
			// disappears; killing only the PTY's group does not reach RPCs.
			cleanup()
			killPTY(t.cmd)
			_ = t.pty.Close()
			<-t.done
		}
	})
}
func validSize(cols, rows int) bool { return cols >= 20 && cols <= 500 && rows >= 5 && rows <= 200 }
func querySize(r *http.Request) (int, int, error) {
	cols, err := strconv.Atoi(r.URL.Query().Get("cols"))
	if err != nil {
		return 0, 0, errors.New("invalid terminal columns")
	}
	rows, err := strconv.Atoi(r.URL.Query().Get("rows"))
	if err != nil || !validSize(cols, rows) {
		return 0, 0, errors.New("terminal size must be 20..500 columns and 5..200 rows")
	}
	return cols, rows, nil
}
