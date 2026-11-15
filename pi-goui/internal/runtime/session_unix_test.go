//go:build unix

package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sessionFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.Mkdir(agent, 0700); err != nil {
		t.Fatal(err)
	}
	return agent, filepath.Join(root, "session.jsonl")
}
func expectSessionBusy(t *testing.T, agent, path string) SessionStatus {
	t.Helper()
	l, err := AcquireSession(agent, path)
	if l != nil {
		_ = l.Close()
		t.Fatal("second writer acquired session")
	}
	var busy *SessionBusyError
	if !errors.Is(err, ErrSessionBusy) || !errors.As(err, &busy) {
		t.Fatalf("want typed busy error, got %v", err)
	}
	return busy.Status
}
func TestSessionProbeIsReadOnly(t *testing.T) {
	agent, path := sessionFixture(t)
	for _, p := range []string{"", path} {
		v, err := ProbeSession(agent, p)
		if err != nil || v.Busy {
			t.Fatalf("probe: %+v %v", v, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(os.Getenv("HOME"), ".pi")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("probe created registry")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("probe created session")
	}
}
func TestSessionExclusiveAliasesAndReplacement(t *testing.T) {
	for _, mode := range []string{"existing", "first-persistence", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			agent, path := sessionFixture(t)
			if mode != "first-persistence" {
				if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lease, err := AcquireSession(agent, path)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			if mode == "first-persistence" {
				if err = os.WriteFile(path, []byte("created"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(filepath.Dir(path), "hardlink.jsonl")
			if err = os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
			if mode == "replacement" {
				replacement := path + ".tmp"
				if err = os.WriteFile(replacement, []byte("replaced"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			otherAgent := t.TempDir()
			for _, candidate := range []string{path, alias} {
				v := expectSessionBusy(t, otherAgent, candidate)
				if v.OwnerPID != os.Getpid() {
					t.Fatalf("owner %+v", v)
				}
			}
			dirAlias := filepath.Join(t.TempDir(), "directory-alias")
			if err = os.Symlink(filepath.Dir(path), dirAlias); err != nil {
				t.Fatal(err)
			}
			expectSessionBusy(t, agent, filepath.Join(dirAlias, filepath.Base(path)))
			key := lease.key
			lockInfo, err := os.Stat(filepath.Join(lease.root, "locks", key+".lock"))
			if err != nil {
				t.Fatal(err)
			}
			if err = lease.Close(); err != nil {
				t.Fatal(err)
			}
			v, err := ProbeSession(agent, path)
			if err != nil || v.Busy {
				t.Fatalf("clean release: %+v %v", v, err)
			}
			next, err := AcquireSession(otherAgent, path)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			nextInfo, err := os.Stat(filepath.Join(next.root, "locks", next.key+".lock"))
			if err != nil || !os.SameFile(lockInfo, nextInfo) {
				t.Fatal("release replaced/unlinked stable lock inode")
			}
		})
	}
}
func TestSessionConcurrentAcquireHasOneWinner(t *testing.T) {
	agent, path := sessionFixture(t)
	var wg sync.WaitGroup
	leases := make(chan *SessionLease, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, e := AcquireSession(agent, path)
			if e == nil {
				leases <- l
			} else if !errors.Is(e, ErrSessionBusy) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	close(leases)
	count := 0
	for l := range leases {
		count++
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("writers=%d", count)
	}
}

func TestSessionReservedFirstInodeCoversOldHardlinkAfterReplacement(t *testing.T) {
	agent, path := sessionFixture(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err = l.prepareFile(); err != nil {
		t.Fatal(err)
	}
	alias := path + ".hardlink"
	if err = os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".replacement", []byte("new inode"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".replacement", path); err != nil {
		t.Fatal(err)
	}
	expectSessionBusy(t, t.TempDir(), alias)
	expectSessionBusy(t, t.TempDir(), path)
}

func TestSessionLockSubstitutionIsRejected(t *testing.T) {
	agent, path := sessionFixture(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lock := filepath.Join(l.root, "locks", l.key+".lock")
	if err = os.Rename(lock, lock+".old"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if another, err := AcquireSession(agent, path); err == nil {
		another.Close()
		t.Fatal("substituted lock allowed another writer")
	}
	// Restore the original inode so this fixture can release normally.
	if err = os.Rename(lock+".old", lock); err != nil {
		t.Fatal(err)
	}
}

func TestSessionFileAppearingBeforeStartupCannotAliasAnotherWriter(t *testing.T) {
	agent, path := sessionFixture(t)
	a, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	other := path + ".other"
	if err = os.WriteFile(other, []byte("other writer history"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := AcquireSession(agent, other)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = os.Link(other, path); err != nil {
		t.Fatal(err)
	}
	if err = a.prepareFile(); err == nil {
		t.Fatal("startup accepted an alias that appeared after acquisition")
	}
	if err = a.abortStart(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(other)
	if err != nil || string(raw) != "other writer history" {
		t.Fatal("failed startup modified another owner's file")
	}
	expectSessionBusy(t, agent, path)
}
func TestSessionRejectsSymlinksAndPrivateRegistryViolations(t *testing.T) {
	agent, path := sessionFixture(t)
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := path + ".symlink"
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireSession(agent, alias); err == nil {
		t.Fatal("session symlink accepted")
	}
	root, err := sessionRegistry(true)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(root, "claims"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = AcquireSession(agent, path); err == nil {
		t.Fatal("public registry accepted")
	}
}
func TestSessionUncleanNeverAutoTakesOver(t *testing.T) {
	agent, path := sessionFixture(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.attach(exec.Command("unused")); err != nil {
		t.Fatal(err)
	}
	if err = l.finish(false, "unverified detached writer"); err == nil {
		t.Fatal("uncertain shutdown reported clean")
	}
	v := expectSessionBusy(t, agent, path)
	if !v.Uncertain {
		t.Fatalf("unconfirmed writer became idle: %+v", v)
	}
	if _, err = os.Stat(filepath.Join(l.root, "claims", l.key+".json")); err != nil {
		t.Fatal("unclean record removed")
	}
}
func TestSessionLegacyLiveWriterFailsClosedWithoutMutation(t *testing.T) {
	agent, path := sessionFixture(t)
	base := filepath.Join(os.Getenv("HOME"), ".pi", "goui-rpc", sessionHash(agent), "executors", "fixture")
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	record := []byte(fmt.Sprintf(`{"PID":%d,"Birth":%q,"Backends":[],"Starting":true}`, os.Getpid(), processBirth(os.Getpid())))
	if err := os.WriteFile(filepath.Join(base, "endpoint.json"), record, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := ProbeSession(agent, path)
	if err != nil || !v.Busy || !v.Uncertain {
		t.Fatalf("legacy probe: %+v %v", v, err)
	}
	expectSessionBusy(t, agent, path)
	after, err := os.ReadFile(filepath.Join(base, "endpoint.json"))
	if err != nil || string(after) != string(record) {
		t.Fatal("modified old registry")
	}
}

// This fixture is a real process using inherited FD 3 and native JSONL pipes.
// It does not load Pi, a model, credentials, or the network.
func TestLeaseBackendProcess(t *testing.T) {
	mode := os.Getenv("GOUI_LEASE_BACKEND")
	if mode == "" {
		return
	}
	path := ""
	for i, a := range os.Args {
		if a == "--session" && i+1 < len(os.Args) {
			path = os.Args[i+1]
		}
	}
	f := os.NewFile(3, "inherited-session-lock")
	_, fdErr := f.Stat()
	if mode == "drop-lock" {
		f.Close()
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "ready", "pid": os.Getpid(), "file": path, "inherited": fdErr == nil})
	if mode == "orphan" || mode == "drop-lock" {
		for {
			time.Sleep(time.Hour)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var c map[string]any
		if json.Unmarshal(scanner.Bytes(), &c) != nil {
			os.Exit(3)
		}
		if c["type"] == "create" {
			if os.WriteFile(path, []byte("latest history"), 0600) != nil {
				os.Exit(4)
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "response", "id": c["id"], "success": true, "data": map[string]any{"file": path}})
	}
	if mode == "slow-cleanup" {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(path+".closed", []byte("flushed"), 0600)
	}
	if mode == "tail-cleanup" {
		// Dispose may emit notifications after the UI stops consuming. The
		// transport must drain them until process exit, not close stdout early.
		for i := 0; i < 512; i++ {
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "cleanup", "text": strings.Repeat("x", 8192)}); err != nil {
				os.Exit(7)
			}
		}
		_ = os.WriteFile(path+".closed", []byte("flushed"), 0600)
	}
	os.Exit(0)
}
func leasedClient(t *testing.T, agent, path, mode string) *Client {
	t.Helper()
	t.Setenv("GOUI_LEASE_BACKEND", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(Config{Bin: exe, AgentDir: agent, SessionFile: path, Args: []string{"-test.run=^TestLeaseBackendProcess$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	select {
	case event := <-c.Events():
		var v struct {
			File      string
			Inherited bool
		}
		if json.Unmarshal(event.Data, &v) != nil || v.File != c.SessionFile() || !v.Inherited {
			t.Fatalf("startup identity/lock: %s", event.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no backend ready event")
	}
	return c
}
func TestSessionStartOwnsFixedPathAndCloseWaitsForCleanup(t *testing.T) {
	agent, _ := sessionFixture(t)
	c := leasedClient(t, agent, "", "slow-cleanup")
	path := c.SessionFile()
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, agent+string(filepath.Separator)) {
		t.Fatalf("new path %q", path)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatal("new session inode was not reserved empty before Pi writes")
	}
	expectSessionBusy(t, agent, path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.Call(ctx, map[string]any{"type": "create"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, map[string]any{"type": "switch_session", "sessionPath": "elsewhere"}); err == nil {
		t.Fatal("backend changed path outside its lease")
	}
	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("Close returned before actual shutdown")
	}
	if _, err := os.Stat(path + ".closed"); err != nil {
		t.Fatal("Close returned before writer cleanup")
	}
	lease, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = lease.Close()
}
func TestSessionStartFailureReleasesReservation(t *testing.T) {
	agent, path := sessionFixture(t)
	if _, err := Start(Config{Bin: filepath.Join(t.TempDir(), "missing-pi"), AgentDir: agent, SessionFile: path}); err == nil {
		t.Fatal("missing executable accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed exec left its empty reservation")
	}
	lease, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = lease.Close()
}

func TestSessionCloseDrainsCleanupOutput(t *testing.T) {
	agent, path := sessionFixture(t)
	c := leasedClient(t, agent, path, "tail-cleanup")
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".closed"); err != nil {
		t.Fatal("shutdown output was cut off before cleanup")
	}
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}
func TestSessionForcedCloseStaysUncertain(t *testing.T) {
	agent, path := sessionFixture(t)
	c := leasedClient(t, agent, path, "orphan")
	if err := c.Close(); err == nil {
		t.Fatal("forced writer exit was reported clean")
	}
	v := expectSessionBusy(t, agent, path)
	if !v.Uncertain || !strings.Contains(v.Reason, "forced") {
		t.Fatalf("shutdown diagnosis lost: %+v", v)
	}
}
