//go:build unix

package runtime

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const sharedLaunch = "--internal-session-broker-launch"
const sharedServe = "--internal-session-broker"

type sharedRecord struct {
	Socket, Token string
	PID           int
	Backends      []int
	Keys          []string
	Birth         string
	BackendBirth  map[int]string
	// Bootstrap fences delayed launchers; Starting is durable before cmd.Start.
	Bootstrap, Starting, Retired bool
	IdentityPending              bool
	// Confirmed by Pi's get_state, never merely a requested launch path. The
	// file may not exist yet; discovery rechecks it for newly created inodes.
	NativePath string
}
type sharedHello struct {
	Token, Action, Client string
	Config                Config
	Environment           []string
}
type sharedWelcome struct {
	Client, Error string
	PID           int
}
type sharedConnection struct {
	record sharedRecord
	id     string
	pid    int
}

func sharedSupported() bool { return true }
func sharedProcessLive(pid int) bool {
	if pid <= 0 || syscall.Kill(pid, 0) != nil {
		return false
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		_, state, _ := strings.Cut(string(data), ") ")
		if strings.HasPrefix(state, "Z ") {
			return false
		}
	}
	return true
}

// Process birth identity prevents stale metadata from mistaking a recycled
// PID for its former executor. Unknown legacy births conservatively fail closed.
func sharedProcessBirth(pid int) string {
	if raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if end := strings.LastIndex(string(raw), ") "); end >= 0 {
			fields := strings.Fields(string(raw)[end+2:])
			if len(fields) > 19 {
				boot, _ := os.ReadFile("/proc/sys/kernel/random/boot_id")
				return "proc:" + strings.TrimSpace(string(boot)) + ":" + fields[19]
			}
		}
	}
	raw, err := exec.Command("/bin/ps", "-p", fmt.Sprint(pid), "-o", "lstart=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
func recordedProcessLive(record sharedRecord, pid int) bool {
	if !sharedProcessLive(pid) {
		return false
	}
	birth := record.BackendBirth[pid]
	if pid == record.PID {
		birth = record.Birth
	}
	if birth == "" {
		return true
	}
	current := sharedProcessBirth(pid)
	return current == "" || current == birth
}
func sharedID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func ownedPrivate(info os.FileInfo, dir bool) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Geteuid() && info.IsDir() == dir && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0077 == 0
}
func sharedRoot(agent string) (string, error) {
	if agent == "" {
		return "", errors.New("shared sessions require an agent directory")
	}
	agent, err := filepath.Abs(agent)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(agent, 0700); err != nil {
		return "", err
	}
	agent, err = filepath.EvalSymlinks(agent)
	if err != nil {
		return "", err
	}
	if err = checkLegacySharedRoot(agent); err != nil {
		return "", err
	}
	base, err := sharedRegistryBase(true)
	if err != nil {
		return "", err
	}
	root := filepath.Join(base, sharedAgentHash(agent))
	if err = checkedSharedDirectory(root, true); err != nil {
		return "", err
	}
	return root, nil
}
func lockSharedRoot(root string) (*os.File, error) {
	fd, err := unix.Open(filepath.Join(root, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "shared-lock")
	info, err := f.Stat()
	if err != nil || !ownedPrivate(info, false) || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid shared RPC lock")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			after, e := os.Lstat(filepath.Join(root, "lock"))
			if e != nil || !os.SameFile(info, after) {
				unlockSharedRoot(f)
				return nil, errors.New("shared lock identity changed")
			}
			return f, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("shared RPC startup lock: %w", err)
		}
		time.Sleep(15 * time.Millisecond)
	}
}
func unlockSharedRoot(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }
func readSharedRecord(root string) (sharedRecord, error) {
	var record sharedRecord
	path := filepath.Join(root, "endpoint.json")
	before, err := os.Lstat(path)
	if err != nil {
		return record, err
	}
	if !ownedPrivate(before, false) || !before.Mode().IsRegular() || before.Size() > 8192 {
		return record, errors.New("invalid shared RPC endpoint")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return record, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		return record, errors.New("shared RPC endpoint identity changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil {
		return record, err
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return record, errors.New("shared RPC endpoint changed")
	}
	if err = json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	if record.Token == "" || record.PID <= 0 || !filepath.IsAbs(record.Socket) || filepath.Base(record.Socket) != "rpc.sock" {
		return record, errors.New("invalid shared RPC endpoint data")
	}
	return record, nil
}
func validSharedSocket(path string) bool {
	if !filepath.IsAbs(path) || filepath.Base(path) != "rpc.sock" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "pi-goui-rpc-") {
		return false
	}
	real, e := filepath.EvalSymlinks(filepath.Dir(path))
	if e != nil || real != filepath.Dir(path) {
		return false
	}
	d, e := os.Lstat(filepath.Dir(path))
	if e != nil || !ownedPrivate(d, true) {
		return false
	}
	f, e := os.Lstat(path)
	return e == nil && ownedPrivate(f, false) && f.Mode()&os.ModeSocket != 0
}
func sharedDial(record sharedRecord) (net.Conn, error) {
	if !validSharedSocket(record.Socket) {
		return nil, errors.New("private RPC socket unavailable")
	}
	return net.DialTimeout("unix", record.Socket, time.Second)
}
func incompleteIdentity(keys []string) bool {
	for _, key := range keys {
		if strings.HasPrefix(key, "inode:") {
			return false
		}
	}
	return true
}

// The launch marker is never removed. Missing metadata is recoverable only if
// nothing could ever have been launched in this directory. Caller holds the
// bootstrap lock. An owner directory without metadata is legacy/unknown, not
// an empty bootstrap: current helpers require a reservation before creating it.
func unstartedExecutor(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "lock" {
			continue // lockSharedRoot already verified this stable inode.
		}
		info, e := entry.Info()
		if !strings.HasPrefix(entry.Name(), "endpoint-") || !strings.HasSuffix(entry.Name(), ".tmp") || e != nil || !ownedPrivate(info, false) || !info.Mode().IsRegular() || info.Size() > 8192 {
			return errors.New("executor metadata missing after launch; ownership unknown")
		}
	}
	return nil
}

func markExecutorLaunched(root string) error {
	path := filepath.Join(root, "launched")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		info, e := os.Lstat(path)
		if e != nil || !ownedPrivate(info, false) || !info.Mode().IsRegular() || info.Size() != 0 {
			return errors.New("unsafe executor launch marker")
		}
		return nil
	}
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close(), syncRuntimeDirectory(root))
}

func ensureSharedBroker(root string, initialKeys ...string) (sharedRecord, error) {
	lock, err := lockSharedRoot(root)
	if err != nil {
		return sharedRecord{}, err
	}
	defer unlockSharedRoot(lock)
	record, readErr := readSharedRecord(root)
	for retry := 0; retry < 3 && readErr != nil && (strings.Contains(readErr.Error(), "identity changed") || strings.Contains(readErr.Error(), "endpoint changed")); retry++ {
		time.Sleep(time.Millisecond)
		record, readErr = readSharedRecord(root)
	}
	if readErr == nil {
		c, e := sharedDial(record)
		if e == nil {
			c.Close()
			return record, nil
		}
		if !record.Bootstrap && !record.Retired && recordedProcessLive(record, record.PID) {
			return record, errors.New("shared RPC broker is unavailable; refusing a duplicate writer")
		}
	}
	if readErr == nil {
		if record.Starting {
			return record, errors.New("session backend start outcome unknown; refusing another writer")
		}
		for _, pid := range record.Backends {
			if recordedProcessLive(record, pid) {
				return record, errors.New("shared RPC broker exited but its backend is still running; refusing a duplicate writer")
			}
		}
	}
	// Invalid existing metadata fails closed. A vanished socket after a clean
	// shutdown is safe only when the descriptor's recorded process is dead.
	if readErr != nil && !os.IsNotExist(readErr) {
		return record, readErr
	}
	if os.IsNotExist(readErr) {
		if err = unstartedExecutor(root); err != nil {
			return record, err
		}
		// This precedes even creation of the lifetime-lock directory, making
		// an unreserved directory distinguishable from lost legacy metadata.
		reservation := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: sharedID(), PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Keys: initialKeys, Bootstrap: true, IdentityPending: incompleteIdentity(initialKeys)}
		if err = publishSharedRecord(root, reservation); err != nil {
			return record, err
		}
	}
	owner := filepath.Join(root, "owner")
	if err = privateDirectory(owner); err != nil {
		return record, err
	}
	guard, err := tryMemberLock(owner)
	if err != nil {
		return record, errors.New("executor still owns its lifetime lock; refusing duplicate writer")
	}
	defer func() {
		if guard != nil {
			unlockSharedRoot(guard)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		return record, err
	}
	// Reserve the identity and generation before launching any helper. A
	// delayed helper can never overwrite a newer reservation or tombstone.
	token := sharedID()
	reservation := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: token, PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Keys: initialKeys, Bootstrap: true, IdentityPending: incompleteIdentity(initialKeys)}
	if err = publishSharedRecord(root, reservation); err != nil {
		return record, err
	}
	if err = markExecutorLaunched(root); err != nil {
		return record, err
	}
	unlockSharedRoot(guard)
	guard = nil
	args := []string{sharedLaunch, root, token}
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	if out, e := cmd.CombinedOutput(); e != nil {
		return record, fmt.Errorf("shared RPC launcher: %w: %s", e, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		record, err = readSharedRecord(root)
		if err == nil {
			if c, e := sharedDial(record); e == nil {
				c.Close()
				return record, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return record, errors.New("shared RPC broker did not become ready")
}
func sharedHandshake(conn net.Conn, hello sharedHello) (sharedWelcome, error) {
	var welcome sharedWelcome
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if err := json.NewEncoder(conn).Encode(hello); err != nil {
		return welcome, err
	}
	// The server waits for the acknowledgement before sending any events, so a
	// decoder cannot read ahead and discard a first event at the transport edge.
	r := bufio.NewReader(conn)
	line, err := readBrokerLine(r, 8192)
	if err != nil {
		return welcome, err
	}
	if len(line) > 8192 {
		return welcome, errors.New("oversized RPC greeting")
	}
	if err = json.Unmarshal(line, &welcome); err != nil {
		return welcome, err
	}
	if welcome.Error != "" {
		return welcome, errors.New(welcome.Error)
	}
	if _, err = conn.Write([]byte("\n")); err != nil {
		return welcome, err
	}
	_ = conn.SetDeadline(time.Time{})
	return welcome, nil
}
func startShared(cfg Config) (*Client, error) {
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
	cfg.Bin, err = filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	root, err := sharedRoot(cfg.AgentDir)
	if err != nil {
		return nil, err
	}
	cfg.AgentDir, err = CanonicalAgentDir(cfg.AgentDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	record, err := discoverExecutor(ctx, root, cfg)
	if err != nil {
		return nil, err
	}
	conn, err := sharedDial(record)
	if err != nil {
		return nil, err
	}
	welcome, err := sharedHandshake(conn, sharedHello{Token: record.Token, Action: "attach", Config: cfg, Environment: childEnvironment(cfg.AgentDir)})
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{stdin: conn, stdout: conn, shared: &sharedConnection{record: record, id: welcome.Client, pid: welcome.PID}, idPrefix: welcome.Client + "-", writes: make(chan *writeRequest, 32), events: make(chan Event), incoming: make(chan Event, 32), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), writeDone: make(chan struct{}), closeDone: make(chan struct{}), pending: make(map[string]chan reply)}
	go c.readLoop()
	go c.writeLoop()
	go c.eventLoop()
	go func() {
		<-c.readDone
		c.mu.Lock()
		if c.exitErr == nil {
			c.exitErr = io.EOF
		}
		c.mu.Unlock()
		close(c.done)
		_ = conn.Close()
	}()
	return c, nil
}
func (s *sharedConnection) mutation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := sharedDial(s.record)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	_, err = sharedHandshake(conn, sharedHello{Token: s.record.Token, Action: "lease", Client: s.id})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		stop()
		conn.Close()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { stop(); conn.Close() }) }, nil
}
func runSharedHelper(args []string) (bool, error) {
	if len(args) == 0 || (args[0] != sharedLaunch && args[0] != sharedServe) {
		return false, nil
	}
	if (len(args) != 2 && len(args) != 3) || !filepath.IsAbs(args[1]) {
		return true, errors.New("invalid session broker arguments")
	}
	root := args[1]
	if err := validateExecutorRegistry(root); err != nil {
		return true, err
	}
	if args[0] == sharedServe {
		return true, serveShared(root, args[2:]...)
	}
	exe, err := os.Executable()
	if err != nil {
		return true, err
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return true, err
	}
	defer null.Close()
	childArgs := append([]string{sharedServe, root}, args[2:]...)
	cmd := exec.Command(exe, childArgs...)
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return true, err
	}
	return true, cmd.Process.Release()
}
