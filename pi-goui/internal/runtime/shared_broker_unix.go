//go:build unix

package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type sessionBroker struct {
	mu          sync.Mutex
	root        string
	record      sharedRecord
	listener    net.Listener
	sessions    map[string]*brokerSession
	clients     map[string]*brokerPeer
	connections map[net.Conn]bool
	pending     int
	lastActive  time.Time
	closing     bool
	wg          sync.WaitGroup
}
type brokerSession struct {
	working         WorkingState
	workingRevision uint64
	broker          *sessionBroker
	backend         *Client
	key, file       string
	clients         map[string]*brokerPeer
	pendingUI       map[string]json.RawMessage
	lease           bool
	closed          bool
	opMu            sync.Mutex
	uiState         map[string]json.RawMessage
	inFlight        map[string]int
}
type brokerPeer struct {
	id      string
	conn    net.Conn
	out     chan json.RawMessage
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	once    sync.Once
	session *brokerSession
}

func (p *brokerPeer) stop() { p.once.Do(func() { close(p.done); p.cancel(); p.conn.Close() }) }
func (p *brokerPeer) send(raw json.RawMessage) {
	select {
	case <-p.done:
		return
	default:
	}
	select {
	case p.out <- bytes.Clone(raw):
	default:
		p.stop()
	}
}
func brokerJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func (s *brokerSession) broadcast(raw json.RawMessage) {
	for _, p := range s.clients {
		p.send(raw)
	}
}

// identity uses both canonical path validation and the kernel file identity, so
// hard-link spellings cannot start a second writer. Missing new files are
// indexed by canonical path until the backend creates them.
func sharedSessionIdentity(file string) (string, string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", "", err
	}
	before, err := os.Lstat(abs)
	if err == nil && (!before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0) {
		return "", "", errors.New("session must be a regular non-symlink file")
	}
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(abs))
	if e != nil {
		return "", "", e
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	after, afterErr := os.Lstat(abs)
	if afterErr == nil {
		if !after.Mode().IsRegular() || after.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("session must be a regular non-symlink file")
		}
		if err == nil && !os.SameFile(before, after) {
			return "", "", errors.New("session identity changed during canonicalization")
		}
		before, err = after, nil
	} else if !os.IsNotExist(afterErr) || err == nil {
		return "", "", fmt.Errorf("session identity changed: %w", afterErr)
	}
	if err == nil {
		st, ok := before.Sys().(*syscall.Stat_t)
		if !ok {
			return "", "", errors.New("session identity unavailable")
		}
		return fmt.Sprintf("inode:%d:%d", st.Dev, st.Ino), abs, nil
	}
	return "path:" + abs, abs, nil
}
func serveShared(root string, initialKeys ...string) error {
	if err := privateDirectory(filepath.Join(root, "owner")); err != nil {
		return err
	}
	owner, err := tryMemberLock(filepath.Join(root, "owner"))
	if err != nil {
		return err
	}
	defer unlockSharedRoot(owner)
	reservation, err := readSharedRecord(root)
	if err != nil || !reservation.Bootstrap || len(initialKeys) != 1 || reservation.Token != initialKeys[0] || reservation.Starting {
		return errors.New("stale or uncertain executor bootstrap")
	}
	initialKeys = reservation.Keys
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "pi-goui-rpc-")
	if err != nil {
		return err
	}
	defer os.Remove(dir)
	socket := filepath.Join(dir, "rpc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	b := &sessionBroker{root: root, listener: listener, record: sharedRecord{Socket: socket, Token: sharedID(), PID: os.Getpid(), Keys: initialKeys, Birth: sharedProcessBirth(os.Getpid()), BackendBirth: map[int]string{}}, sessions: map[string]*brokerSession{}, clients: map[string]*brokerPeer{}, connections: map[net.Conn]bool{}, lastActive: time.Now()}
	b.record.IdentityPending = reservation.IdentityPending
	if err = publishSharedRecord(root, b.record); err != nil {
		return err
	}
	// Retain the authenticated descriptor as a tombstone after exit. Removing
	// it would make an orderly shutdown indistinguishable from lost ownership
	// metadata while a writer is still alive. PID birth identities fence reuse.
	done := make(chan struct{})
	defer close(done)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		case <-done:
		}
	}()
	go func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				b.mu.Lock()
				// The executor, not the coordinator, is the recoverable authority.
				if _, err := os.Lstat(filepath.Join(root, "endpoint.json")); os.IsNotExist(err) {
					_ = b.persistRecord()
				}
				if len(b.clients) == 0 && b.pending == 0 && len(b.sessions) == 0 && time.Since(b.lastActive) > 2*time.Second {
					b.closing = true
					listener.Close()
					b.mu.Unlock()
					return
				}
				b.mu.Unlock()
			}
		}
	}()
	for {
		conn, e := listener.Accept()
		if e != nil {
			break
		}
		b.mu.Lock()
		if b.closing {
			b.mu.Unlock()
			conn.Close()
			continue
		}
		b.connections[conn] = true
		b.pending++
		b.lastActive = time.Now()
		b.wg.Add(1)
		b.mu.Unlock()
		go func() { defer b.wg.Done(); b.handle(conn) }()
	}
	b.mu.Lock()
	b.closing = true
	for conn := range b.connections {
		conn.Close()
	}
	seen := map[*brokerSession]bool{}
	for _, s := range b.sessions {
		if !seen[s] {
			seen[s] = true
			s.closed = true
			stopBrokerBackend(s.backend)
		}
	}
	b.mu.Unlock()
	b.wg.Wait()
	return nil
}
func (b *sessionBroker) handle(conn net.Conn) {
	defer conn.Close()
	defer func() { b.mu.Lock(); delete(b.connections, conn); b.mu.Unlock() }()
	reader := bufio.NewReaderSize(conn, 64*1024)
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	line, err := readBrokerLine(reader, 1<<20)
	var hello sharedHello
	if err == nil {
		err = json.Unmarshal(line, &hello)
	}
	b.mu.Lock()
	b.pending--
	b.lastActive = time.Now()
	if err != nil || hello.Token != b.record.Token || b.closing {
		b.mu.Unlock()
		return
	}
	if hello.Action == "lease" {
		b.lease(conn, reader, hello)
		return
	}
	if hello.Action != "attach" {
		b.mu.Unlock()
		return
	}
	hello.Config.environment = hello.Environment
	p, s, err := b.attach(hello.Config, conn)
	if err != nil {
		b.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(sharedWelcome{Error: err.Error()})
		return
	}
	b.mu.Unlock()
	defer b.detach(p)
	if err = json.NewEncoder(conn).Encode(sharedWelcome{Client: p.id, PID: s.backend.PID()}); err != nil {
		return
	}
	if _, err = readBrokerLine(reader, 1); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-p.done:
				return
			case data := <-p.out:
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, e := conn.Write(append(data, '\n')); e != nil {
					p.stop()
					return
				}
			}
		}
	}()
	defer func() { p.stop(); <-writerDone }()
	slots := make(chan struct{}, 16)
	var calls sync.WaitGroup
	defer func() { p.stop(); calls.Wait() }()
	for {
		line, err = readBrokerLine(reader, 64<<20)
		if err != nil {
			return
		}
		var command map[string]any
		if json.Unmarshal(line, &command) != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		case <-p.done:
			return
		}
		written := make(chan struct{})
		calls.Add(1)
		go func() { defer calls.Done(); defer func() { <-slots }(); b.command(p, command, written) }()
		select {
		case <-written:
		case <-p.done:
			return
		}
	}
}
func readBrokerLine(r *bufio.Reader, limit int) ([]byte, error) {
	var out []byte
	for {
		part, prefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		if len(out)+len(part) > limit {
			return nil, errors.New("shared RPC frame too large")
		}
		out = append(out, part...)
		if !prefix {
			return out, nil
		}
	}
}

// attach is serialized across every request, including initial get_state and
// alias registration. The global startup lock handles independent processes.
func (b *sessionBroker) attach(cfg Config, conn net.Conn) (*brokerPeer, *brokerSession, error) {
	if cfg.NoSession {
		return nil, nil, errors.New("nonpersistent sessions cannot be shared")
	}
	agent, err := executorAgent(b.root, cfg.AgentDir)
	if err != nil {
		return nil, nil, err
	}
	cfg.AgentDir = agent
	var key, path string
	if cfg.SessionFile != "" {
		key, path, err = sharedSessionIdentity(cfg.SessionFile)
		if err != nil {
			return nil, nil, err
		}
		cfg.SessionFile = path
	}
	s := b.sessions[key]
	if s == nil && path != "" {
		s = b.sessions["path:"+path]
	}
	if s == nil && path != "" {
		seen := map[*brokerSession]bool{}
		for _, candidate := range b.sessions {
			if seen[candidate] || candidate.closed || candidate.lease {
				continue
			}
			seen[candidate] = true
			candidateFile := candidate.file
			if candidateFile == "" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				raw, e := candidate.backend.Call(ctx, map[string]any{"type": "get_state"})
				cancel()
				if e != nil {
					return nil, nil, fmt.Errorf("cannot verify existing shared session: %w", e)
				}
				var state struct{ SessionFile string }
				if json.Unmarshal(raw, &state) != nil || state.SessionFile == "" {
					continue
				}
				candidateFile = state.SessionFile
			}
			candidateKey, candidatePath, e := sharedSessionIdentity(candidateFile)
			if e != nil {
				return nil, nil, e
			}
			if candidateKey == key || candidatePath == path {
				if candidate.file == "" {
					b.removeSession(candidate)
					candidate.key, candidate.file = candidateKey, candidatePath
					b.record.NativePath = candidatePath
				}
				s = candidate
				b.sessions[key] = s
				b.sessions["path:"+path] = s
				break
			}
		}
	}
	if s != nil && s.key != key && path != "" {
		nativeKey, _, nativeErr := sharedSessionIdentity(s.file)
		if strings.HasPrefix(s.key, "path:") && nativeErr == nil && nativeKey == key {
			b.removeSession(s)
			s.key = key
			b.sessions[key] = s
			b.sessions["path:"+path] = s
		} else {
			return nil, nil, errors.New("shared session file identity changed; close the previous backend before reopening")
		}
	}
	if s != nil && (s.lease || s.closed) {
		return nil, nil, errors.New("session is being changed or closed; retry after the operation finishes")
	}
	if s == nil && len(b.sessions) != 0 {
		return nil, nil, errors.New("executor belongs to another session")
	}
	if s == nil {
		cfg.relayExtensions = true
		if b.record.Starting {
			return nil, nil, errors.New("previous backend start outcome unknown")
		}
		// A process may outlive the executor even before its PID is returned.
		// Persist uncertainty before cmd.Start; a crashed executor cannot erase
		// that window by publishing an empty replacement record.
		b.record.Starting = true
		b.record.NativePath = ""
		if e := b.persistRecord(); e != nil {
			return nil, nil, e
		}
		backend, e := startDirect(cfg)
		if e != nil {
			// startDirect returns an error only before a successful cmd.Start.
			b.record.Starting = false
			return nil, nil, errors.Join(e, b.persistRecord())
		}
		s = &brokerSession{working: newSharedWorkingState(), broker: b, backend: backend, key: key, file: path, clients: map[string]*brokerPeer{}, pendingUI: map[string]json.RawMessage{}, uiState: map[string]json.RawMessage{}, inFlight: map[string]int{}}
		b.record.Starting = false
		b.record.Backends = append(b.record.Backends, backend.PID())
		b.record.BackendBirth[backend.PID()] = sharedProcessBirth(backend.PID())
		if e = b.persistRecord(); e != nil {
			backend.Close()
			return nil, nil, e
		}
		go s.events()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		state, e := backend.Call(ctx, map[string]any{"type": "get_state"})
		cancel()
		if e != nil {
			backend.Close()
			return nil, nil, fmt.Errorf("shared session state: %w", e)
		}
		var info struct{ SessionFile string }
		if e = json.Unmarshal(state, &info); e != nil {
			backend.Close()
			return nil, nil, e
		}
		if info.SessionFile != "" {
			key, path, e = sharedSessionIdentity(info.SessionFile)
			if e != nil {
				backend.Close()
				return nil, nil, e
			}
			b.record.NativePath = path
		}
		if path == "" {
			key = "new:" + sharedID()
		}
		if existing := b.sessions[key]; existing != nil && existing != s {
			backend.Close()
			return nil, nil, errors.New("backend unexpectedly opened an already shared session")
		}
		s.key, s.file = key, path
		b.sessions[key] = s
		if path != "" {
			b.sessions["path:"+path] = s
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &brokerPeer{id: sharedID(), conn: conn, out: make(chan json.RawMessage, 256), done: make(chan struct{}), ctx: ctx, cancel: cancel, session: s}
	s.clients[p.id] = p
	b.clients[p.id] = p
	// Registration, state replay and subsequent events share b.mu. The bounded
	// peer queue disconnects on overflow, never silently loses an event.
	p.send(brokerJSON(map[string]any{"type": "goui_session_snapshot", "initial": true}))
	// Queue the compact projection under the same lock as live publication.
	if view := s.working.View(); view.Present {
		view.Revision = s.workingRevision
		p.send(withWorkingSnapshot(brokerJSON(map[string]any{"type": "goui_working_snapshot"}), view))
	}
	for _, raw := range s.uiState {
		p.send(raw)
	}
	for _, raw := range s.pendingUI {
		p.send(raw)
	}
	b.record.Keys = []string{s.key}
	b.record.IdentityPending = incompleteIdentity(b.record.Keys)
	if s.file != "" {
		b.record.Keys = append(b.record.Keys, "path:"+s.file)
	}
	if err := b.persistRecord(); err != nil {
		delete(s.clients, p.id)
		delete(b.clients, p.id)
		p.stop()
		if len(s.clients) == 0 && !s.lease {
			s.closed = true
			b.removeSession(s)
			stopBrokerBackend(s.backend)
		}
		return nil, nil, err
	}
	return p, s, nil
}
func (b *sessionBroker) persistRecord() error {
	live := b.record.Backends[:0]
	for _, pid := range b.record.Backends {
		if recordedProcessLive(b.record, pid) {
			live = append(live, pid)
		} else {
			delete(b.record.BackendBirth, pid)
		}
	}
	b.record.Backends = live
	return publishSharedRecord(b.root, b.record)
}

func (b *sessionBroker) removeSession(s *brokerSession) {
	for key, value := range b.sessions {
		if value == s {
			delete(b.sessions, key)
		}
	}
}
func (b *sessionBroker) detach(p *brokerPeer) {
	p.stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	s := p.session
	delete(s.clients, p.id)
	delete(b.clients, p.id)
	b.lastActive = time.Now()
	if len(s.clients) == 0 && !s.lease {
		s.closed = true
		b.removeSession(s)
		stopBrokerBackend(s.backend)
	}
}
func stopBrokerBackend(c *Client) {
	if c == nil {
		return
	}
	cleanup := sharedDescendants(c.PID())
	_ = c.Close()
	cleanup()
}
func (b *sessionBroker) lease(conn net.Conn, reader *bufio.Reader, hello sharedHello) {
	// Called with b.mu held; keep the session registered after detach so another
	// frontend cannot start a writer during local rename/delete.
	p := b.clients[hello.Client]
	if p == nil || p.session.closed || p.session.lease || len(p.session.clients) != 1 {
		b.mu.Unlock()
		_ = json.NewEncoder(conn).Encode(sharedWelcome{Error: "session is in use by another goui instance or unavailable"})
		return
	}
	s := p.session
	s.lease = true
	stopBrokerBackend(s.backend)
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		s.lease = false
		s.closed = true
		b.removeSession(s)
		for _, peer := range s.clients {
			peer.stop()
		}
		b.lastActive = time.Now()
		b.mu.Unlock()
	}()
	if json.NewEncoder(conn).Encode(sharedWelcome{}) != nil {
		return
	}
	if _, err := readBrokerLine(reader, 1); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	_, _ = io.Copy(io.Discard, reader)
}
func (b *sessionBroker) command(p *brokerPeer, command map[string]any, written chan<- struct{}) {
	kind, _ := command["type"].(string)
	p.session.opMu.Lock()
	var once sync.Once
	release := func() { once.Do(func() { p.session.opMu.Unlock(); close(written) }) }
	defer release()
	id, _ := command["id"].(string)
	respond := func(data json.RawMessage, err error) {
		if id == "" {
			return
		}
		v := map[string]any{"type": "response", "id": id, "command": kind, "success": err == nil}
		if err != nil {
			v["error"] = err.Error()
		} else {
			v["data"] = data
		}
		p.send(brokerJSON(v))
	}
	b.mu.Lock()
	s := p.session
	if s.closed || s.lease {
		b.mu.Unlock()
		respond(nil, errors.New("shared session is closed for a file operation"))
		return
	}
	if kind == "extension_ui_response" {
		request, _ := command["id"].(string)
		if _, ok := s.pendingUI[request]; !ok {
			b.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := s.backend.Send(ctx, command)
		cancel()
		if err == nil {
			delete(s.pendingUI, request)
			s.broadcast(brokerJSON(map[string]any{"type": "goui_session_ui_resolved", "id": request}))
		}
		b.mu.Unlock()
		return
	}
	// UI session/tab changes create or attach a separate client. A shared
	// backend cannot be redirected underneath other views by raw RPC commands.
	if (strings.HasPrefix(kind, "goui_") && kind != "goui_submit_prompt") || kind == "new_session" || kind == "switch_session" || kind == "fork" {
		b.mu.Unlock()
		respond(nil, errors.New("shared sessions must be opened as separate goui tabs"))
		return
	}
	backend := s.backend
	s.inFlight[p.id]++
	s.updateEventOwner()
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		s.inFlight[p.id]--
		if s.inFlight[p.id] == 0 {
			delete(s.inFlight, p.id)
		}
		s.updateEventOwner()
		b.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(p.ctx, 45*time.Second)
	defer cancel()
	var data json.RawMessage
	var err error
	if kind == "goui_submit_prompt" {
		payload, _ := command["payload"].(map[string]any)
		draft, _ := command["draft"].(string)
		merge, _ := command["mergeQueued"].(bool)
		restore, submitErr := backend.submitPrompt(ctx, payload, draft, merge, release)
		message := ""
		if submitErr != nil {
			message = submitErr.Error()
		}
		data = brokerJSON(map[string]any{"restoreDraft": restore, "error": message})
		err = nil
	} else {
		data, err = backend.call(ctx, command, release)
	}
	release()
	if err == nil && ctx.Err() == nil && (kind == "prompt" || kind == "goui_submit_prompt") {
		s.registerPath()
	}
	respond(data, err)
	if err != nil || strings.HasPrefix(kind, "get_") {
		return
	}
	if kind == "prompt" {
		message, _ := command["message"].(string)
		if strings.HasPrefix(message, "/"+extensionCommand+" ") {
			// Bridge reads (including opaque future methods) must not trigger
			// refresh loops. Its supported *.set mutations invalidate snapshots;
			// other extensions can publish their native session/UI events.
			var request struct{ Method string }
			if json.Unmarshal([]byte(strings.TrimPrefix(message, "/"+extensionCommand+" ")), &request) != nil || !strings.HasSuffix(request.Method, ".set") {
				return
			}
		}
	}
	b.mu.Lock()
	if !s.closed {
		s.broadcast(brokerJSON(map[string]any{"type": "goui_session_changed"}))
	}
	b.mu.Unlock()
}

// Pi's uncorrelated editor commands are local only when a single origin is
// unambiguous. Never guess a peer when concurrent callers could own the event.
func (s *brokerSession) updateEventOwner() {
	if len(s.inFlight) == 1 {
		for id := range s.inFlight {
			scope := id
			s.backend.eventOwner.Store(&scope)
		}
		return
	}
	s.backend.eventOwner.Store(nil)
}
func (s *brokerSession) events() {
	for event := range s.backend.Events() {
		b := s.broker
		b.mu.Lock()
		if !s.closed && event.Err != nil {
			s.broadcast(brokerJSON(map[string]any{"type": "goui_rpc_error", "message": event.Err.Error()}))
		}
		if !s.closed && len(event.Data) > 0 {
			s.working.Apply(event.Data, time.Now())
			s.workingRevision++
			view := s.working.View()
			view.Revision = s.workingRevision
			forwarded := withWorkingSnapshot(event.Data, view)
			var envelope struct{ Type, Method, ID string }
			_ = json.Unmarshal(event.Data, &envelope)
			if envelope.Type == "extension_ui_request" && envelope.ID != "" && (envelope.Method == "select" || envelope.Method == "confirm" || envelope.Method == "input" || envelope.Method == "editor") {
				s.pendingUI[envelope.ID] = bytes.Clone(event.Data)
			}
			if envelope.Type == "extension_ui_request" && envelope.Method == "set_editor_text" {
				if owner := s.clients[event.owner]; owner != nil {
					owner.send(forwarded)
				}
			} else {
				if envelope.Type == "extension_ui_request" && (envelope.Method == "setStatus" || envelope.Method == "setWidget" || envelope.Method == "setTitle") {
					var fields map[string]any
					_ = json.Unmarshal(event.Data, &fields)
					key, _ := fields["key"].(string)
					if key == "" {
						key, _ = fields["statusKey"].(string)
					}
					if key == "" {
						key, _ = fields["widgetKey"].(string)
					}
					s.uiState[envelope.Method+":"+key] = bytes.Clone(event.Data)
				}
				s.broadcast(forwarded)
			}
		}
		b.mu.Unlock()
		// A newly created native session may get its path only after its first turn.
		if len(event.Data) > 0 && (bytes.Contains(event.Data, []byte(`"agent_end"`)) || bytes.Contains(event.Data, []byte(`"agent_settled"`))) {
			s.registerPath()
		}
	}
	b := s.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	if !s.lease && !s.closed {
		s.closed = true
		b.removeSession(s)
		for _, p := range s.clients {
			p.stop()
		}
	}
}
func (s *brokerSession) registerPath() {
	// Internal refreshes obey the same transaction fence as client writes.
	s.opMu.Lock()
	defer s.opMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := s.backend.Call(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		return
	}
	var state struct{ SessionFile string }
	if json.Unmarshal(raw, &state) != nil || state.SessionFile == "" {
		return
	}
	key, path, err := sharedSessionIdentity(state.SessionFile)
	if err != nil {
		return
	}
	b := s.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.closed || s.lease {
		return
	}
	if existing := b.sessions[key]; existing != nil && existing != s {
		s.closed = true
		b.removeSession(s)
		for _, p := range s.clients {
			p.stop()
		}
		go stopBrokerBackend(s.backend)
		return
	}
	if s.file != "" && (s.file != path || s.key != key) {
		b.removeSession(s)
	}
	s.key, s.file = key, path
	b.sessions[key] = s
	b.sessions["path:"+path] = s
	b.record.Keys = []string{key, "path:" + path}
	b.record.NativePath = path
	b.record.IdentityPending = incompleteIdentity(b.record.Keys)
	if err := b.persistRecord(); err != nil {
		for _, p := range s.clients {
			p.stop()
		}
	}
}

// Snapshot descendants before closing Pi, including tools with independent
// process groups. Only the captured descendants of this backend are signaled.
func sharedDescendants(pid int) func() {
	raw, err := exec.Command("/bin/ps", "-eo", "pid=,ppid=,lstart=").Output()
	if err != nil {
		return func() {}
	}
	parents := map[int]int{}
	identities := map[int]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		var child, parent int
		fields := strings.Fields(line)
		if _, e := fmt.Sscan(line, &child, &parent); e == nil && len(fields) >= 7 {
			parents[child] = parent
			identities[child] = strings.Join(fields[2:], " ")
		}
	}
	var descendants []int
	for child := range parents {
		seen := map[int]bool{}
		for current := child; current > 1 && !seen[current]; current = parents[current] {
			seen[current] = true
			if parents[current] == pid {
				descendants = append(descendants, child)
				break
			}
		}
	}
	return func() {
		// Recheck process birth times after waiting for Pi, rather than killing
		// an unrelated process if a captured PID has already been recycled.
		now, err := exec.Command("/bin/ps", "-eo", "pid=,lstart=").Output()
		if err != nil {
			return
		}
		current := map[int]string{}
		for _, line := range strings.Split(string(now), "\n") {
			var pid int
			fields := strings.Fields(line)
			if _, e := fmt.Sscan(line, &pid); e == nil && len(fields) >= 6 {
				current[pid] = strings.Join(fields[1:], " ")
			}
		}
		for _, child := range descendants {
			if current[child] != "" && current[child] == identities[child] {
				_ = syscall.Kill(child, syscall.SIGKILL)
			}
		}
	}
}
