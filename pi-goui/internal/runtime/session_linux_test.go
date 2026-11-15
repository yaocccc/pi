//go:build linux

package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSessionOwnerProcess(t *testing.T) {
	if os.Getenv("GOUI_SESSION_OWNER_HELPER") == "" {
		return
	}
	exe, _ := os.Executable()
	c, err := Start(Config{Bin: exe, AgentDir: os.Getenv("GOUI_SESSION_AGENT"), SessionFile: os.Getenv("GOUI_SESSION_PATH"), Args: []string{"-test.run=^TestLeaseBackendProcess$", "--"}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	event := <-c.Events()
	var ready struct{ PID int }
	if json.Unmarshal(event.Data, &ready) != nil || ready.PID <= 0 {
		os.Exit(3)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"pid": ready.PID, "lock": filepath.Join(c.lease.root, "locks", c.lease.key+".lock")})
	for {
		time.Sleep(time.Hour)
	}
}

func TestSessionParentCrashKeepsWriterExclusive(t *testing.T) {
	// Both an ordinary inherited descriptor and an unverified launcher that
	// deliberately closes it must block a second writer after parent SIGKILL.
	for _, mode := range []string{"orphan", "drop-lock"} {
		t.Run(mode, func(t *testing.T) {
			agent, path := sessionFixture(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestSessionOwnerProcess$", "--")
			cmd.Env = append(os.Environ(), "GOUI_SESSION_OWNER_HELPER=1", "GOUI_SESSION_AGENT="+agent, "GOUI_SESSION_PATH="+path, "GOUI_LEASE_BACKEND="+mode)
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			type result struct {
				PID  int
				Lock string
			}
			ch := make(chan result, 1)
			go func() { var r result; _ = json.NewDecoder(bufio.NewReader(out)).Decode(&r); ch <- r }()
			var child result
			select {
			case child = <-ch:
			case <-time.After(5 * time.Second):
				t.Fatal("owner did not start")
			}
			if child.PID <= 0 {
				t.Fatal("missing child PID")
			}
			defer syscall.Kill(child.PID, syscall.SIGKILL)
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			if !processLive(child.PID) {
				t.Fatal("fixture writer did not survive its parent")
			}
			f, e := lockSessionFile(child.Lock, false)
			if mode == "orphan" {
				if f != nil {
					f.Close()
				}
				if !lockContended(e) {
					t.Fatalf("actual writer lost inherited OS lock: %v", e)
				}
			} else {
				if e != nil {
					t.Fatalf("fixture did not release inherited lock: %v", e)
				}
				f.Close()
			}
			expectSessionBusy(t, agent, path)
			_ = syscall.Kill(child.PID, syscall.SIGKILL)
			deadline := time.Now().Add(3 * time.Second)
			for processLive(child.PID) && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			before, err := os.Stat(child.Lock)
			if err != nil {
				t.Fatal(err)
			}
			waitSessionRecovery(t, agent, path)
			next, err := AcquireSession(agent, path)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			after, err := os.Stat(child.Lock)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("recovery replaced the stable lock inode")
			}
		})
	}
}
