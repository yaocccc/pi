//go:build unix

package runtime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// SessionMember belongs to an actual TUI, never to a PTY host or executor.
// The flock is the term: it is never replaced or stolen on a timeout. Closing
// waits for control handlers before releasing it, fencing every old-term write.
type SessionMember struct {
	root string
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func JoinSessionMember(agent string) (*SessionMember, error) {
	root, err := sharedRoot(agent)
	if err != nil {
		return nil, err
	}
	// An old agent-wide daemon may still own a writer. Never migrate it live.
	if old, e := readSharedRecord(root); e == nil {
		if recordedProcessLive(old, old.PID) {
			return nil, errors.New("legacy shared broker is live; close old TUIs before joining")
		}
		for _, pid := range old.Backends {
			if recordedProcessLive(old, pid) {
				return nil, errors.New("legacy backend may be live")
			}
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	for _, dir := range []string{"coordinator", "executors"} {
		if err = privateDirectory(filepath.Join(root, dir)); err != nil {
			return nil, err
		}
	}
	m := &SessionMember{root: root, stop: make(chan struct{}), done: make(chan struct{})}
	go m.run()
	return m, nil
}
func privateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !ownedPrivate(info, true) {
		return fmt.Errorf("unsafe runtime directory %s", path)
	}
	return nil
}
func (m *SessionMember) Close() error { m.once.Do(func() { close(m.stop) }); <-m.done; return nil }
func (m *SessionMember) run() {
	defer close(m.done)
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		f, err := tryMemberLock(filepath.Join(m.root, "coordinator"))
		if err == nil {
			m.lead(f)
			unlockSharedRoot(f)
		}
		select {
		case <-m.stop:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func tryMemberLock(root string) (*os.File, error) {
	path := filepath.Join(root, "lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, e := f.Stat()
	after, e2 := os.Lstat(path)
	if e != nil || e2 != nil || !ownedPrivate(info, false) || !info.Mode().IsRegular() || !os.SameFile(info, after) {
		f.Close()
		return nil, errors.New("unsafe coordinator lock")
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	after, e = os.Lstat(path)
	if e != nil || !os.SameFile(info, after) {
		unlockSharedRoot(f)
		return nil, errors.New("coordinator lock identity changed")
	}
	return f, nil
}
func (m *SessionMember) lead(lock *os.File) {
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return
	}
	dir, err := os.MkdirTemp(base, "pi-goui-rpc-")
	if err != nil {
		return
	}
	defer os.Remove(dir)
	socket := filepath.Join(dir, "rpc.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		return
	}
	defer l.Close()
	if os.Chmod(socket, 0600) != nil {
		return
	}
	record := sharedRecord{Socket: socket, Token: sharedID(), PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid())}
	root := filepath.Join(m.root, "coordinator")
	if err = publishSharedRecord(root, record); err != nil {
		return
	}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-m.stop:
			l.Close()
		case <-stopped:
		}
	}()
	var handlers sync.WaitGroup
	defer handlers.Wait()
	// Only discovery/startup is serialized. Pi RPC never uses this lock/socket.
	var control sync.Mutex
	for {
		conn, e := l.Accept()
		if e != nil {
			return
		}
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
			raw, e := readBrokerLine(bufio.NewReader(conn), 1<<20)
			if e != nil {
				return
			}
			var hello sharedHello
			if json.Unmarshal(raw, &hello) != nil || hello.Token != record.Token || hello.Action != "resolve" {
				return
			}
			control.Lock()
			defer control.Unlock()
			select {
			case <-m.stop:
				return
			default:
			}
			info, e := lock.Stat()
			current, statErr := os.Lstat(filepath.Join(root, "lock"))
			if e != nil || statErr != nil || !os.SameFile(info, current) {
				return
			}
			target, e := resolveExecutor(m.root, hello.Config)
			reply := controlReply{Record: target}
			if e != nil {
				reply.Error = e.Error()
				reply.Retry = errors.Is(e, errExecutorIdentityPending)
			}
			_ = json.NewEncoder(conn).Encode(reply)
		}()
	}
}

type controlReply struct {
	Record sharedRecord
	Error  string
	Retry  bool // Discovery only: never replay a Pi operation.
}

var errExecutorIdentityPending = errors.New("executor identity pending; retry discovery")

func publishSharedRecord(root string, record sharedRecord) error {
	path := filepath.Join(root, "endpoint.json")
	before, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if statErr == nil && (!ownedPrivate(before, false) || !before.Mode().IsRegular() || before.Size() > 8192) {
		return errors.New("unsafe runtime descriptor")
	}
	data := brokerJSON(record)
	if len(data) > 8192 {
		return errors.New("runtime descriptor exceeds size limit")
	}
	tmp := filepath.Join(root, "endpoint-"+sharedID()+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	_, err = f.Write(data)
	err = errors.Join(err, f.Sync(), f.Close())
	if err == nil {
		after, e := os.Lstat(path)
		if (statErr == nil && (e != nil || !os.SameFile(before, after))) || (statErr != nil && !os.IsNotExist(e)) {
			return errors.New("runtime descriptor identity changed")
		}
		err = os.Rename(tmp, path)
		if err == nil {
			err = syncRuntimeDirectory(root)
		}
	}
	return err
}
func syncRuntimeDirectory(root string) error {
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func emptyExecutor(root string) error {
	lock, err := lockSharedRoot(root)
	if err != nil {
		return err
	}
	defer unlockSharedRoot(lock)
	if _, err = readSharedRecord(root); !os.IsNotExist(err) {
		return errors.New("executor metadata changed during discovery; retry")
	}
	return unstartedExecutor(root)
}

// Retire a launcher reservation which never acquired the lifetime lock. The
// generation change fences even a delayed helper from the previous leader.
// Unlike an unknown backend start, this state proves no Pi could have started.
func retireBootstrap(root string) (sharedRecord, error) {
	lock, err := lockSharedRoot(root)
	if err != nil {
		return sharedRecord{}, err
	}
	defer unlockSharedRoot(lock)
	if err = privateDirectory(filepath.Join(root, "owner")); err != nil {
		return sharedRecord{}, err
	}
	owner, err := tryMemberLock(filepath.Join(root, "owner"))
	if err != nil {
		return readSharedRecord(root) // A helper is actively publishing its identity.
	}
	defer unlockSharedRoot(owner)
	record, err := readSharedRecord(root)
	if err != nil || !record.Bootstrap {
		return record, err
	}
	if record.Starting || len(record.Backends) != 0 {
		return record, errors.New("bootstrap contains uncertain backend ownership")
	}
	record.Bootstrap, record.Retired = false, true
	record.Token = sharedID()
	return record, publishSharedRecord(root, record)
}

func resolveExecutor(root string, cfg Config) (sharedRecord, error) {
	var zero sharedRecord
	key := "new:" + sharedID()
	path := ""
	var err error
	if cfg.SessionFile != "" {
		key, path, err = sharedSessionIdentity(cfg.SessionFile)
		if err != nil {
			return zero, err
		}
	}
	hash := sha256.Sum256([]byte(key))
	target := filepath.Join(root, "executors", hex.EncodeToString(hash[:]))
	dirs, err := os.ReadDir(filepath.Join(root, "executors"))
	if err != nil {
		return zero, err
	}
	identityPending, retryIdentity := false, true
	for _, dir := range dirs {
		if !dir.IsDir() || len(dir.Name()) != 64 {
			return zero, errors.New("invalid executor directory")
		}
		target := filepath.Join(root, "executors", dir.Name())
		if err = privateDirectory(target); err != nil {
			return zero, err
		}
		record, e := readSharedRecord(target)
		for retry := 0; retry < 5 && e != nil && (strings.Contains(e.Error(), "identity changed") || strings.Contains(e.Error(), "endpoint changed")); retry++ {
			time.Sleep(time.Millisecond)
			record, e = readSharedRecord(target)
		}
		if os.IsNotExist(e) {
			if e = emptyExecutor(target); e != nil {
				return zero, e
			}
			continue // mkdir interrupted before any launch; no ownership lost.
		}
		if e != nil {
			return zero, e
		}
		for _, alias := range record.Keys {
			if alias == key || (path != "" && alias == "path:"+path) {
				return ensureSharedBroker(target, key)
			}
		}
		if record.Bootstrap {
			record, e = retireBootstrap(target)
			if e != nil {
				return zero, e
			}
		}
		// A newly bootstrapped executor must publish its native identity before
		// another pathname can be opened. Fail closed rather than guess ownership.
		uncertain := record.Bootstrap || record.Starting || (!record.Retired && recordedProcessLive(record, record.PID))
		for _, pid := range record.Backends {
			uncertain = uncertain || recordedProcessLive(record, pid)
		}
		if uncertain && (record.IdentityPending || incompleteIdentity(record.Keys)) {
			// Pi's confirmed native pathname scopes an unwritten session. Re-stat
			// it before excluding this executor: its file and hard-link aliases
			// may exist before the next identity publication. A requested history
			// must already have an inode to be proven unrelated; two absent paths
			// could still acquire the same inode during startup.
			if !record.Starting && !record.Bootstrap && record.NativePath != "" && recordedProcessLive(record, record.PID) {
				nativeKey, nativePath, nativeErr := sharedSessionIdentity(record.NativePath)
				if nativeErr == nil {
					if nativeKey == key || nativePath == path {
						return ensureSharedBroker(target, key)
					}
					if strings.HasPrefix(key, "inode:") {
						continue
					}
				}
			}
			identityPending = true
			retryIdentity = retryIdentity && (record.Bootstrap || recordedProcessLive(record, record.PID))
		}
	}
	// Existing aliases above remain usable; an explicitly new session cannot
	// resume an unknown writer. Only an unmatched persistent identity is fenced.
	if path != "" && identityPending {
		if retryIdentity {
			return zero, errExecutorIdentityPending
		}
		// A dead executor's orphan/unknown startup is not a transient failure.
		return zero, errors.New(errExecutorIdentityPending.Error())
	}
	if err = privateDirectory(target); err != nil {
		return zero, err
	}
	return ensureSharedBroker(target, key)
}
func discoverExecutor(ctx context.Context, root string, cfg Config) (sharedRecord, error) {
	var last error
	for {
		select {
		case <-ctx.Done():
			return sharedRecord{}, fmt.Errorf("coordinator unavailable: %w (%v)", ctx.Err(), last)
		default:
		}
		record, e := readSharedRecord(filepath.Join(root, "coordinator"))
		if e == nil {
			conn, err := sharedDial(record)
			if err == nil {
				_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
				e = json.NewEncoder(conn).Encode(sharedHello{Token: record.Token, Action: "resolve", Config: cfg})
				if e == nil {
					raw, err := readBrokerLine(bufio.NewReader(conn), 8192)
					e = err
					var reply controlReply
					if e == nil {
						e = json.Unmarshal(raw, &reply)
					}
					conn.Close()
					if e == nil {
						if reply.Error == "" {
							return reply.Record, nil
						}
						if !reply.Retry {
							return sharedRecord{}, errors.New(reply.Error)
						}
						e = errors.New(reply.Error)
					}
				} else {
					conn.Close()
				}
			} else {
				e = err
			}
		}
		last = e
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Millisecond):
		}
	}
}
