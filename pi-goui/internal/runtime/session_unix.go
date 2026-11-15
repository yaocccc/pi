//go:build unix

package runtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type fileIdentity struct{ Device, Inode uint64 }
type sessionClaim struct {
	Token, Path, Agent, Birth, LauncherBirth string
	OwnerPID, LauncherPID                    int
	Identity, LockIdentity                   fileIdentity
	Unclean                                  bool
	Reason                                   string
}

// SessionLease owns one stable lock inode. Closing a parent descriptor never
// explicitly unlocks it: an inherited descriptor in Pi must retain ownership.
type SessionLease struct {
	mu               sync.Mutex
	root, key        string
	file             *os.File
	claim            sessionClaim
	closed           bool
	created, spawned bool
}

func sessionHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func sessionIdentity(path string) (fileIdentity, error) {
	i, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileIdentity{}, nil
	}
	if err != nil {
		return fileIdentity{}, err
	}
	if !i.Mode().IsRegular() {
		return fileIdentity{}, errors.New("session file must be regular and cannot be a symlink")
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, errors.New("session file identity unavailable")
	}
	return fileIdentity{uint64(s.Dev), uint64(s.Ino)}, nil
}
func openFileIdentity(f *os.File) (fileIdentity, error) {
	info, err := f.Stat()
	if err != nil {
		return fileIdentity{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, errors.New("file identity unavailable")
	}
	return fileIdentity{uint64(st.Dev), uint64(st.Ino)}, nil
}
func sameIdentity(a, b fileIdentity) bool { return a.Inode != 0 && a == b }
func ownedPrivate(info os.FileInfo, dir bool) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(s.Uid) == os.Geteuid() && info.IsDir() == dir && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0077 == 0
}
func checkedSessionDir(path string, create, private bool) error {
	if create {
		if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	mask := os.FileMode(0022)
	if private {
		mask = 0077
	}
	if !ok || int(s.Uid) != os.Geteuid() || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&mask != 0 {
		return fmt.Errorf("unsafe session registry directory %q", path)
	}
	return nil
}
func sessionRegistry(create bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return "", errors.New("session registry requires an absolute home directory")
	}
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	if err = checkedSessionDir(home, false, false); err != nil {
		return "", err
	}
	parent := filepath.Join(home, ".pi")
	if err = checkedSessionDir(parent, create, false); err != nil {
		return "", err
	}
	root := filepath.Join(parent, "goui-locks")
	for _, path := range []string{root, filepath.Join(root, "locks"), filepath.Join(root, "claims")} {
		if err = checkedSessionDir(path, create, true); err != nil {
			return "", err
		}
	}
	return root, nil
}

// openSessionFile refuses substituted inodes and never follows symlinks. It
// also serves read-only probes; flock does not require write access on Unix.
func openSessionFile(path string, create bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if create {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	i, e := f.Stat()
	after, e2 := os.Lstat(path)
	if e != nil || e2 != nil || !ownedPrivate(i, false) || !i.Mode().IsRegular() || !os.SameFile(i, after) {
		f.Close()
		return nil, fmt.Errorf("unsafe session registry file %q", path)
	}
	return f, nil
}
func lockSessionFile(path string, create bool) (*os.File, error) {
	f, err := openSessionFile(path, create)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	i, err := f.Stat()
	after, e := os.Lstat(path)
	if err != nil || e != nil || !os.SameFile(i, after) {
		f.Close()
		return nil, errors.New("session lock identity changed")
	}
	return f, nil
}
func registryGuard(root string) (*os.File, error) { return existingRegistryGuard(root, true) }

func existingRegistryGuard(root string, create bool) (*os.File, error) {
	deadline := time.Now().Add(time.Second)
	for {
		f, err := lockSessionFile(filepath.Join(root, "registry.lock"), create)
		if !lockContended(err) {
			return f, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("session registry is busy; retry the operation")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func lockContended(err error) bool {
	return errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN)
}

func readSessionJSON(path string, value any) error {
	f, err := openSessionFile(path, false)
	if err != nil {
		return err
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil {
		return err
	}
	if i.Size() > 16384 {
		return errors.New("oversized session ownership record")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return err
	}
	if len(raw) > 16384 {
		return errors.New("oversized session ownership record")
	}
	return json.Unmarshal(raw, value)
}
func publishSessionClaim(root, key string, claim sessionClaim) error {
	raw, err := json.Marshal(claim)
	if err != nil {
		return err
	}
	if len(raw) > 16384 {
		return errors.New("session ownership record is too large")
	}
	dir := filepath.Join(root, "claims")
	f, err := os.CreateTemp(dir, ".claim-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, key+".json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func claimStatus(c sessionClaim, uncertain bool, reason string) SessionStatus {
	return SessionStatus{Busy: true, Uncertain: uncertain, OwnerPID: c.OwnerPID, LauncherPID: c.LauncherPID, SessionFile: c.Path, Reason: reason}
}

// Recover only while holding both the registry guard and the original OS lock.
// An absent launcher identity means the owner may have died during exec/start;
// an explicit cleanup failure may describe a detached, untracked writer.
func recoverSessionClaim(root, key string, c sessionClaim, lock *os.File) (SessionStatus, error) {
	defer lock.Close()
	identity, err := openFileIdentity(lock)
	if err != nil || !sameIdentity(identity, c.LockIdentity) {
		return SessionStatus{}, errors.New("session lock inode changed during recovery")
	}
	path := filepath.Join(root, "claims", key+".json")
	var current sessionClaim
	if err = readSessionJSON(path, &current); err != nil {
		return SessionStatus{}, err
	}
	if current != c {
		return SessionStatus{}, errors.New("session ownership changed during recovery")
	}
	if c.Unclean && c.Reason != "" {
		return claimStatus(c, true, c.Reason), nil
	}
	if !sessionProcessGone(c.OwnerPID, c.Birth) {
		return claimStatus(c, true, "session owner is live or its identity is unknown despite the released lock"), nil
	}
	if c.Unclean {
		if c.LauncherPID <= 0 || c.LauncherBirth == "" {
			return claimStatus(c, true, "previous writer startup was not confirmed; writer identity is unknown"), nil
		}
		if !sessionProcessGone(c.LauncherPID, c.LauncherBirth) {
			return claimStatus(c, true, "previous writer is live or its identity cannot be verified despite the released lock"), nil
		}
		if err = sessionProcessGroupGone(c.LauncherPID, c.LauncherBirth); err != nil {
			return claimStatus(c, true, err.Error()), nil
		}
	}
	if err = os.Remove(path); err != nil {
		return SessionStatus{}, err
	}
	return SessionStatus{}, nil
}

// Caller holds the registry guard. Only active/unclean claims are enumerated,
// never the accumulated lock files. Restatting the owner's original path
// recognizes first persistence; its initial inode still covers old hardlinks.
func probeClaims(root, path string, identity fileIdentity) (SessionStatus, error) {
	entries, err := os.ReadDir(filepath.Join(root, "claims"))
	if err != nil {
		return SessionStatus{}, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".claim-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return SessionStatus{}, errors.New("invalid session ownership registry entry")
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		var c sessionClaim
		if err = readSessionJSON(filepath.Join(root, "claims", entry.Name()), &c); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return SessionStatus{}, err
		}
		if c.Token == "" || c.Path == "" || c.OwnerPID <= 0 || sessionHash(c.Path) != key {
			return SessionStatus{}, errors.New("invalid session ownership record")
		}
		current, err := sessionIdentity(c.Path)
		if err != nil {
			return SessionStatus{}, err
		}
		if c.Path != path && !sameIdentity(c.Identity, identity) && !sameIdentity(current, identity) {
			continue
		}
		lockPath := filepath.Join(root, "locks", key+".lock")
		lockIdentity, err := sessionIdentity(lockPath)
		if err != nil {
			return SessionStatus{}, err
		}
		if !sameIdentity(c.LockIdentity, lockIdentity) {
			return SessionStatus{}, errors.New("session lock inode changed")
		}
		f, err := lockSessionFile(lockPath, false)
		if lockContended(err) {
			if !sameProcess(c.OwnerPID, c.Birth) {
				return claimStatus(c, true, "owner exited while a writer still holds the session lock"), nil
			}
			if c.Reason != "" {
				return claimStatus(c, true, c.Reason), nil
			}
			return claimStatus(c, false, "another instance owns the session"), nil
		}
		if err != nil {
			return SessionStatus{}, err
		}
		if status, err := recoverSessionClaim(root, key, c, f); err != nil || status.Busy {
			return status, err
		}
	}
	// A missing claim cannot override a held OS lock for this known path.
	f, err := lockSessionFile(filepath.Join(root, "locks", sessionHash(path)+".lock"), false)
	if lockContended(err) {
		return SessionStatus{Busy: true, Uncertain: true, SessionFile: path, Reason: "session lock held without confirmed owner metadata"}, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return SessionStatus{}, err
	}
	if f != nil {
		f.Close()
	}
	return SessionStatus{SessionFile: path}, nil
}

// ProbeSession never starts Pi, creates registry files/directories or changes
// permissions. It may recover a proven-dead claim under the existing locks.
// Busy=false is only a display hint, never write authorization.
func ProbeSession(agentDir, path string) (SessionStatus, error) {
	if path == "" {
		return SessionStatus{}, nil
	}
	path, err := sessionPath(path)
	if err != nil {
		return SessionStatus{}, err
	}
	agent, err := sessionAgent(agentDir)
	if err != nil {
		return SessionStatus{}, err
	}
	if status, err := probeLegacySessions(agent, path); err != nil || status.Busy {
		return status, err
	}
	identity, err := sessionIdentity(path)
	if err != nil {
		return SessionStatus{}, err
	}
	root, err := sessionRegistry(false)
	if errors.Is(err, os.ErrNotExist) {
		return SessionStatus{SessionFile: path}, nil
	}
	if err != nil {
		return SessionStatus{}, err
	}
	guard, err := existingRegistryGuard(root, false)
	if err != nil {
		return SessionStatus{}, err
	}
	defer guard.Close()
	return probeClaims(root, path, identity)
}

// AcquireSession atomically claims a native session for either one Pi lifetime
// or a short file mutation. The session lock is nonblocking, including when the
// competing instance was started with a different agent configuration.
func AcquireSession(agentDir, path string) (*SessionLease, error) {
	path, err := sessionPath(path)
	if err != nil {
		return nil, err
	}
	agent, err := sessionAgent(agentDir)
	if err != nil {
		return nil, err
	}
	root, err := sessionRegistry(true)
	if err != nil {
		return nil, err
	}
	guard, err := registryGuard(root)
	if err != nil {
		return nil, err
	}
	defer guard.Close()
	if status, err := probeLegacySessions(agent, path); err != nil {
		return nil, err
	} else if status.Busy {
		return nil, &SessionBusyError{status}
	}
	identity, err := sessionIdentity(path)
	if err != nil {
		return nil, err
	}
	if status, err := probeClaims(root, path, identity); err != nil {
		return nil, err
	} else if status.Busy {
		return nil, &SessionBusyError{status}
	}
	key := sessionHash(path)
	f, err := lockSessionFile(filepath.Join(root, "locks", key+".lock"), true)
	if lockContended(err) {
		return nil, &SessionBusyError{SessionStatus{Busy: true, Uncertain: true, SessionFile: path, Reason: "session lock held without confirmed owner metadata"}}
	}
	if err != nil {
		return nil, err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		f.Close()
		return nil, err
	}
	c := sessionClaim{Token: hex.EncodeToString(token[:]), Path: path, Agent: agent, OwnerPID: os.Getpid(), Birth: processBirth(os.Getpid()), Identity: identity}
	c.LockIdentity, err = openFileIdentity(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	if err = publishSessionClaim(root, key, c); err != nil {
		f.Close()
		return nil, err
	}
	return &SessionLease{root: root, key: key, file: f, claim: c}, nil
}

// Reserve the first inode while registration is serialized. Pi supports an
// empty explicit session file and initializes its native header itself. This
// closes the creation/hardlink/replacement gap before any writer is launched.
func (l *SessionLease) prepareFile() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	guard, err := registryGuard(l.root)
	if err != nil {
		return err
	}
	defer guard.Close()
	fd, err := unix.Open(l.claim.Path, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err == nil {
		f := os.NewFile(uintptr(fd), l.claim.Path)
		l.claim.Identity, err = openFileIdentity(f)
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
		l.created = true
	} else if errors.Is(err, unix.EEXIST) {
		identity, e := sessionIdentity(l.claim.Path)
		if e != nil {
			return e
		}
		if l.claim.Identity.Inode == 0 || !sameIdentity(identity, l.claim.Identity) {
			return errors.New("session file changed before Pi startup")
		}
		l.claim.Identity = identity
	} else {
		return err
	}
	return publishSessionClaim(l.root, l.key, l.claim)
}
func (l *SessionLease) abortStart() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	if l.spawned {
		return errors.New("started writer requires process cleanup")
	}
	if l.created {
		identity, err := sessionIdentity(l.claim.Path)
		info, e := os.Lstat(l.claim.Path)
		if err == nil && e == nil && sameIdentity(identity, l.claim.Identity) && info.Size() == 0 {
			_ = os.Remove(l.claim.Path)
		}
	}
	return l.finishLocked(true, "")
}

// Close ends a short mutation. A lease attached to Pi is released only by the
// Client after process cleanup, never by an early call to this method.
func (l *SessionLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	if l.claim.Unclean {
		return errors.New("Pi session lease requires confirmed process shutdown")
	}
	return l.finishLocked(true, "")
}
func (l *SessionLease) attach(cmd *exec.Cmd) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.claim.Unclean = true
	l.claim.Reason = ""
	if err := publishSessionClaim(l.root, l.key, l.claim); err != nil {
		return err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, l.file)
	return nil
}
func (l *SessionLease) started(pid int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spawned = true
	l.claim.LauncherPID, l.claim.LauncherBirth = pid, processBirth(pid)
	return publishSessionClaim(l.root, l.key, l.claim)
}
func (l *SessionLease) finish(clean bool, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.finishLocked(clean, reason)
}
func (l *SessionLease) finishLocked(clean bool, reason string) error {
	if l.closed {
		return nil
	}
	l.closed = true
	guard, err := registryGuard(l.root)
	if err != nil {
		l.file.Close()
		return err
	}
	defer guard.Close()
	var observed sessionClaim
	if err = readSessionJSON(filepath.Join(l.root, "claims", l.key+".json"), &observed); err != nil || observed.Token != l.claim.Token {
		l.file.Close()
		if err != nil {
			return err
		}
		return errors.New("session ownership changed before shutdown")
	}
	if !clean {
		l.claim.Unclean, l.claim.Reason = true, reason
		if err = publishSessionClaim(l.root, l.key, l.claim); err != nil {
			l.file.Close()
			return err
		}
	}
	// No LOCK_UN: a surviving child shares this open file description.
	if err = l.file.Close(); err != nil {
		return err
	}
	check, err := lockSessionFile(filepath.Join(l.root, "locks", l.key+".lock"), false)
	if lockContended(err) {
		l.claim.Unclean, l.claim.Reason = true, "session writer still holds inherited lock after launcher cleanup"
		_ = publishSessionClaim(l.root, l.key, l.claim)
		return errors.New("session writer still holds inherited lock; ownership remains uncertain")
	}
	if err != nil {
		return err
	}
	defer check.Close()
	identity, err := openFileIdentity(check)
	if err != nil {
		return err
	}
	if !sameIdentity(identity, l.claim.LockIdentity) {
		return errors.New("session lock inode changed during shutdown")
	}
	if !clean {
		return fmt.Errorf("session remains read-only after unconfirmed shutdown: %s", reason)
	}
	var current sessionClaim
	path := filepath.Join(l.root, "claims", l.key+".json")
	if err = readSessionJSON(path, &current); err != nil {
		return err
	}
	if current.Token != l.claim.Token {
		return errors.New("session ownership changed during shutdown")
	}
	return os.Remove(path)
}
