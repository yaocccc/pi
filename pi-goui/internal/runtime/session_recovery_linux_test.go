//go:build linux

package runtime

import (
	"bufio"
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
	"testing"
	"time"
)

func waitSessionRecovery(t *testing.T, agent, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		v, err := ProbeSession(agent, path)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("exited writer did not recover: %+v", v)
		}
		// A dying multithreaded process can expose a zombie leader before
		// its last descriptor reference is gone. The OS lock still wins.
		time.Sleep(10 * time.Millisecond)
	}
}

// Leave exactly the metadata a killed owner leaves after a recorded launcher
// exits. The real process identity is sampled before Wait reaps it.
func expiredSessionClaim(t *testing.T) (string, string, *SessionLease) {
	t.Helper()
	agent, path := sessionFixture(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/true")
	configureProcess(cmd)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	birth := processBirth(cmd.Process.Pid)
	if err = cmd.Wait(); err != nil || birth == "" {
		t.Fatalf("sample exited process: %q %v", birth, err)
	}
	l.claim.OwnerPID, l.claim.LauncherPID = cmd.Process.Pid, cmd.Process.Pid
	l.claim.Birth, l.claim.LauncherBirth = birth, birth
	l.claim.Unclean = true
	if err = publishSessionClaim(l.root, l.key, l.claim); err != nil {
		t.Fatal(err)
	}
	if err = l.file.Close(); err != nil {
		t.Fatal(err)
	}
	l.closed = true
	return agent, path, l
}

func TestSessionRecoveryEvidence(t *testing.T) {
	for _, name := range []string{"recorded exit", "reused owner PID", "reused launcher PID", "reused launcher process group", "unknown owner birth", "incomparable owner birth", "missing boot identity", "unknown launcher birth", "unrecorded exec", "live owner", "live launcher", "detached cleanup failure", "short mutation"} {
		t.Run(name, func(t *testing.T) {
			agent, path, l := expiredSessionClaim(t)
			wantBusy := true
			unrelatedPID := 0
			switch name {
			case "recorded exit":
				wantBusy = false
			case "reused owner PID":
				l.claim.OwnerPID = os.Getpid()
				l.claim.Birth = "proc:previous-boot:1"
				wantBusy = false
			case "reused launcher PID", "reused launcher process group":
				unrelated := exec.Command("/bin/sleep", "30")
				if name == "reused launcher process group" {
					configureProcess(unrelated)
				}
				if err := unrelated.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
				unrelatedPID = unrelated.Process.Pid
				l.claim.LauncherPID = unrelatedPID
				l.claim.LauncherBirth = "proc:previous-boot:1"
				wantBusy = false
			case "unknown owner birth":
				l.claim.Birth = ""
			case "incomparable owner birth":
				l.claim.OwnerPID, l.claim.Birth = os.Getpid(), "ps identity unavailable from proc"
			case "missing boot identity":
				l.claim.LauncherBirth = "proc::123"
			case "unknown launcher birth":
				l.claim.LauncherBirth = ""
			case "unrecorded exec":
				l.claim.LauncherPID = 0
				l.claim.LauncherBirth = ""
			case "live owner":
				l.claim.OwnerPID, l.claim.Birth = os.Getpid(), processBirth(os.Getpid())
			case "live launcher":
				l.claim.LauncherPID, l.claim.LauncherBirth = os.Getpid(), processBirth(os.Getpid())
			case "detached cleanup failure":
				l.claim.Reason = "unverified detached writer"
			case "short mutation":
				l.claim.Unclean = false
				l.claim.LauncherPID, l.claim.LauncherBirth = 0, ""
				wantBusy = false
			}
			if err := publishSessionClaim(l.root, l.key, l.claim); err != nil {
				t.Fatal(err)
			}
			v, err := ProbeSession(agent, path)
			if err != nil || v.Busy != wantBusy || (wantBusy && !v.Uncertain) {
				t.Fatalf("probe: %+v %v, want busy=%v", v, err, wantBusy)
			}
			if unrelatedPID != 0 && !processLive(unrelatedPID) {
				t.Fatal("recovery disturbed an unrelated reused PID")
			}
			_, err = os.Stat(filepath.Join(l.root, "claims", l.key+".json"))
			if wantBusy && err != nil || !wantBusy && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrong claim retention: %v", err)
			}
		})
	}
}

func TestSessionRecoveryDirectAcquire(t *testing.T) {
	agent, path, old := expiredSessionClaim(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if l.claim.Token == old.claim.Token || !sameIdentity(l.claim.LockIdentity, old.claim.LockIdentity) {
		t.Fatal("takeover reused the stale token or replaced the lock inode")
	}
}

func TestSessionRecoveryConcurrentProbeAndAcquire(t *testing.T) {
	agent, path, old := expiredSessionClaim(t)
	var wg sync.WaitGroup
	leases := make(chan *SessionLease, 12)
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				if _, err := ProbeSession(agent, path); err != nil {
					t.Error(err)
				}
			}
			l, err := AcquireSession(agent, path)
			if err == nil {
				leases <- l
			} else if !errors.Is(err, ErrSessionBusy) {
				t.Error(err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(leases)
	count := 0
	for l := range leases {
		count++
		defer l.Close()
		var current sessionClaim
		if err := readSessionJSON(filepath.Join(l.root, "claims", l.key+".json"), &current); err != nil || current.Token != l.claim.Token || current.Token == old.claim.Token {
			t.Fatalf("recovery removed or replaced the winning claim: %+v %v", current, err)
		}
		if !sameIdentity(current.LockIdentity, old.claim.LockIdentity) {
			t.Fatal("recovery replaced lock inode")
		}
	}
	if count != 1 {
		t.Fatalf("writers=%d", count)
	}
}

// Two independent processes contend only after both have reached the barrier.
func TestSessionTakeoverProcess(t *testing.T) {
	if os.Getenv("GOUI_TAKEOVER_HELPER") == "" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode("ready")
	input := bufio.NewScanner(os.Stdin)
	if !input.Scan() {
		os.Exit(2)
	}
	l, err := AcquireSession(os.Getenv("GOUI_SESSION_AGENT"), os.Getenv("GOUI_SESSION_PATH"))
	if err != nil {
		_ = encoder.Encode(map[string]any{"busy": errors.Is(err, ErrSessionBusy), "error": err.Error()})
		os.Exit(0)
	}
	_ = encoder.Encode(map[string]any{"token": l.claim.Token})
	input.Scan() // Keep the winner locked until the parent checks its claim.
	if err = l.Close(); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestSessionRecoveryTwoProcessTakeover(t *testing.T) {
	agent, path, old := expiredSessionClaim(t)
	type contender struct {
		cmd    *exec.Cmd
		input  io.WriteCloser
		output *json.Decoder
	}
	var contenders []contender
	for i := 0; i < 2; i++ {
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestSessionTakeoverProcess$", "--")
		cmd.Env = append(os.Environ(), "GOUI_TAKEOVER_HELPER=1", "GOUI_SESSION_AGENT="+agent, "GOUI_SESSION_PATH="+path)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
		decoder := json.NewDecoder(output)
		var ready string
		if err = decoder.Decode(&ready); err != nil || ready != "ready" {
			t.Fatalf("takeover barrier: %q %v", ready, err)
		}
		contenders = append(contenders, contender{cmd, input, decoder})
	}
	for _, c := range contenders {
		if _, err := fmt.Fprintln(c.input, "acquire"); err != nil {
			t.Fatal(err)
		}
	}
	winners := 0
	for _, c := range contenders {
		var result struct {
			Token, Error string
			Busy         bool
		}
		if err := c.output.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Token == "" {
			if !result.Busy {
				t.Fatalf("takeover failed without contention: %s", result.Error)
			}
			continue
		}
		winners++
		v, err := ProbeSession(agent, path)
		if err != nil || !v.Busy || v.OwnerPID != c.cmd.Process.Pid {
			t.Fatalf("winner lost ownership: %+v %v", v, err)
		}
		var claim sessionClaim
		if err = readSessionJSON(filepath.Join(old.root, "claims", old.key+".json"), &claim); err != nil || claim.Token != result.Token || claim.Token == old.claim.Token || !sameIdentity(claim.LockIdentity, old.claim.LockIdentity) {
			t.Fatalf("winner claim/lock changed: %+v %v", claim, err)
		}
	}
	if winners != 1 {
		t.Fatalf("writers=%d", winners)
	}
	for _, c := range contenders {
		_ = c.input.Close()
		if err := c.cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionRecoveryNeverDeletesOrOverwritesNewClaim(t *testing.T) {
	_, _, old := expiredSessionClaim(t)
	path := filepath.Join(old.root, "claims", old.key+".json")
	newClaim := old.claim
	newClaim.Token = "replacement-owner-token"
	if err := publishSessionClaim(old.root, old.key, newClaim); err != nil {
		t.Fatal(err)
	}
	guard, err := registryGuard(old.root)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockSessionFile(filepath.Join(old.root, "locks", old.key+".lock"), false)
	if err != nil {
		guard.Close()
		t.Fatal(err)
	}
	_, err = recoverSessionClaim(old.root, old.key, old.claim, lock)
	guard.Close()
	if err == nil {
		t.Fatal("stale recovery observation accepted replacement claim")
	}
	lock, err = lockSessionFile(filepath.Join(old.root, "locks", old.key+".lock"), false)
	if err != nil {
		t.Fatal(err)
	}
	stale := &SessionLease{root: old.root, key: old.key, file: lock, claim: old.claim}
	if err = stale.finish(false, "old shutdown failure"); err == nil {
		t.Fatal("stale shutdown accepted replacement claim")
	}
	var current sessionClaim
	if err = readSessionJSON(path, &current); err != nil || current != newClaim {
		t.Fatalf("new claim was deleted or overwritten: %+v %v", current, err)
	}
}

func TestSessionProbeRespectsLockWithoutClaim(t *testing.T) {
	agent, path := sessionFixture(t)
	l, err := AcquireSession(agent, path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.file.Close()
	if err = os.Remove(filepath.Join(l.root, "claims", l.key+".json")); err != nil {
		t.Fatal(err)
	}
	v, err := ProbeSession(agent, path)
	if err != nil || !v.Busy || !v.Uncertain {
		t.Fatalf("metadata absence overrode held OS lock: %+v %v", v, err)
	}
	expectSessionBusy(t, agent, path)
}

// A live child stays in its old launcher's process group, with or without FD 3.
func TestRecoveryGroupProcess(t *testing.T) {
	mode := os.Getenv("GOUI_RECOVERY_GROUP")
	if mode == "" {
		return
	}
	if mode == "child" {
		if os.Getenv("GOUI_RECOVERY_DROP") == "1" {
			_ = os.NewFile(3, "session-lock").Close()
		}
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	exe, _ := os.Executable()
	child := exec.Command(exe, "-test.run=^TestRecoveryGroupProcess$", "--")
	child.Env = append(os.Environ(), "GOUI_RECOVERY_GROUP=child")
	if mode == "inherit" {
		child.ExtraFiles = []*os.File{os.NewFile(3, "session-lock")}
	} else {
		child.Env = append(child.Env, "GOUI_RECOVERY_DROP=1")
	}
	ready, err := child.StdoutPipe()
	if err != nil {
		os.Exit(2)
	}
	if err = child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !bufio.NewScanner(ready).Scan() {
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(child.Process.Pid)
	bufio.NewScanner(os.Stdin).Scan()
	os.Exit(0)
}

func TestSessionRecoveryWaitsForOrphanGroupMembers(t *testing.T) {
	for _, mode := range []string{"inherit", "drop"} {
		t.Run(mode, func(t *testing.T) {
			agent, path, l := expiredSessionClaim(t)
			lock, err := lockSessionFile(filepath.Join(l.root, "locks", l.key+".lock"), false)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			exe, _ := os.Executable()
			cmd := exec.Command(exe, "-test.run=^TestRecoveryGroupProcess$", "--")
			cmd.Env = append(os.Environ(), "GOUI_RECOVERY_GROUP="+mode)
			cmd.ExtraFiles = []*os.File{lock}
			configureProcess(cmd)
			input, _ := cmd.StdinPipe()
			defer input.Close()
			output, _ := cmd.StdoutPipe()
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			var child int
			if err = json.NewDecoder(output).Decode(&child); err != nil || child <= 0 {
				t.Fatalf("group child: %d %v", child, err)
			}
			birth := processBirth(child)
			defer func() {
				if sameProcess(child, birth) {
					_ = syscall.Kill(child, syscall.SIGKILL)
				}
			}()
			l.claim.LauncherPID, l.claim.LauncherBirth = cmd.Process.Pid, processBirth(cmd.Process.Pid)
			if err = publishSessionClaim(l.root, l.key, l.claim); err != nil {
				t.Fatal(err)
			}
			lock.Close()
			input.Close()
			if err = cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			v := expectSessionBusy(t, agent, path)
			if !v.Uncertain || mode == "drop" && !strings.Contains(v.Reason, "process group") {
				t.Fatalf("live orphan missed: %+v", v)
			}
			_ = syscall.Kill(child, syscall.SIGKILL)
			deadline := time.Now().Add(3 * time.Second)
			for processLive(child) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			waitSessionRecovery(t, agent, path)
		})
	}
}
