//go:build unix

package runtime

import (
	"bufio"
	"context"
	"encoding/json"
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

type memberChild struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	pid    int
	waited bool
}

func launchMemberChild(t *testing.T, cfg Config) *memberChild {
	t.Helper()
	exe, _ := os.Executable()
	raw, _ := json.Marshal(cfg)
	cmd := exec.Command(exe, "-test.run=^TestSharedClientProcess$", "--")
	cmd.Env = append(os.Environ(), "GOUI_SHARED_CLIENT_CONFIG="+string(raw))
	in, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	c := &memberChild{cmd: cmd, in: in, out: bufio.NewReader(out)}
	t.Cleanup(func() {
		if !c.waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	})
	return c
}
func (c *memberChild) ready(t *testing.T) {
	t.Helper()
	line, e := c.out.ReadBytes('\n')
	if e != nil {
		t.Fatal(e)
	}
	var info struct{ PID int }
	if e = json.Unmarshal(line, &info); e != nil {
		t.Fatal(e)
	}
	c.pid = info.PID
}
func (c *memberChild) prompt(t *testing.T) {
	t.Helper()
	fmt.Fprintln(c.in, "surviving-member")
	line, e := c.out.ReadString('\n')
	if e != nil || line != "ok\n" {
		t.Fatalf("surviving member: %q %v", line, e)
	}
}
func (c *memberChild) close(t *testing.T) {
	t.Helper()
	c.in.Close()
	e := c.cmd.Wait()
	c.waited = true
	if e != nil {
		t.Fatal(e)
	}
}

func TestMembersColdStartElectionAndStableExecutor(t *testing.T) {
	for _, kill := range []bool{false, true} {
		t.Run(fmt.Sprint("kill=", kill), func(t *testing.T) {
			cfg, root := sharedFixtureMember(t, false)
			// Both actual member processes start before either is ready; the test
			// parent never participates in election and the fake Pi is a third process.
			a := launchMemberChild(t, cfg)
			b := launchMemberChild(t, cfg)
			a.ready(t)
			b.ready(t)
			if a.pid != b.pid {
				t.Fatal("duplicate Pi on concurrent cold start")
			}
			registry := filepath.Join(testSharedRoot(t, cfg.AgentDir), "coordinator")
			record, e := readSharedRecord(registry)
			if e != nil {
				t.Fatal(e)
			}
			leader, survivor := a, b
			if record.PID == b.cmd.Process.Pid {
				leader, survivor = b, a
			}
			if record.PID != leader.cmd.Process.Pid {
				t.Fatal("non-member became leader")
			}
			before, e := os.Stat(filepath.Join(registry, "lock"))
			if e != nil {
				t.Fatal(e)
			}
			if e = leader.cmd.Process.Signal(syscall.SIGSTOP); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { leader.cmd.Process.Signal(syscall.SIGCONT) })
			time.Sleep(200 * time.Millisecond)
			after, e := readSharedRecord(registry)
			if e != nil || after.Token != record.Token {
				t.Fatal("stopped leader was replaced", e)
			}
			survivor.prompt(t) // Data path works while coordinator is frozen.
			if e = leader.cmd.Process.Signal(syscall.SIGCONT); e != nil {
				t.Fatal(e)
			}
			if kill {
				leader.cmd.Process.Kill()
				leader.cmd.Wait()
				leader.waited = true
			} else {
				leader.close(t)
			}
			sharedEventually(t, func() bool {
				r, e := readSharedRecord(registry)
				return e == nil && r.PID == survivor.cmd.Process.Pid && r.Token != record.Token
			})
			afterLock, e := os.Stat(filepath.Join(registry, "lock"))
			if e != nil || !os.SameFile(before, afterLock) {
				t.Fatal("lock inode replaced", e)
			}
			survivor.prompt(t)
			current, e := readSharedRecord(registry)
			if e != nil {
				t.Fatal(e)
			}
			stale, e := sharedDial(current)
			if e != nil {
				t.Fatal(e)
			}
			stale.SetDeadline(time.Now().Add(time.Second))
			json.NewEncoder(stale).Encode(sharedHello{Token: record.Token, Action: "resolve", Config: cfg})
			var staleReply controlReply
			if e = json.NewDecoder(stale).Decode(&staleReply); e == nil {
				stale.Close()
				t.Fatal("old epoch authorized control")
			}
			stale.Close()
			// The successor discovers the existing executor from private metadata.
			c := launchMemberChild(t, cfg)
			c.ready(t)
			if c.pid != a.pid {
				t.Fatal("leader transition changed Pi PID")
			}
			executors := filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors")
			dirs, e := os.ReadDir(executors)
			if e != nil || len(dirs) != 1 {
				t.Fatal("executor registry", e)
			}
			executor, e := readSharedRecord(filepath.Join(executors, dirs[0].Name()))
			if e != nil {
				t.Fatal(e)
			}
			survivor.close(t)
			c.prompt(t)
			c.close(t)
			sharedEventually(t, func() bool { return sharedGone(a.pid) && sharedGone(executor.PID) })
			wire, _ := os.ReadFile(filepath.Join(root, "wire"))
			if strings.Count(string(wire), `"kind":"start"`) != 1 {
				t.Fatal("expected exactly one backend start")
			}
		})
	}
}

func TestExecutorsIsolationAndOrderedSubmit(t *testing.T) {
	cfg, root := sharedFixture(t)
	a, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	other := cfg
	other.SessionFile = filepath.Join(root, "copy.jsonl")
	contents, _ := os.ReadFile(cfg.SessionFile)
	if e = os.WriteFile(other.SessionFile, contents, 0600); e != nil {
		t.Fatal(e)
	}
	c, e := startShared(other)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if a.PID() == c.PID() || a.shared.record.PID == c.shared.record.PID {
		t.Fatal("copied session shared an executor")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	slow := make(chan error, 1)
	go func() { _, e := a.Call(ctx, map[string]any{"type": "fixture_slow"}); slow <- e }()
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	sharedCall(t, c, map[string]any{"type": "get_state"})
	if time.Since(start) > 400*time.Millisecond {
		t.Fatal("cross-session RPC blocked")
	}
	if e = <-slow; e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for _, client := range []*Client{a, b} {
		wg.Add(1)
		go func(client *Client) {
			defer wg.Done()
			_, err := client.SubmitPrompt(ctx, map[string]any{"message": "draft"}, "draft", true)
			failures <- err
		}(client)
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	wire, e := os.ReadFile(filepath.Join(root, "wire"))
	if e != nil {
		t.Fatal(e)
	}
	var sequence []string
	previous := ""
	for _, line := range strings.Split(string(wire), "\n") {
		var v struct {
			PID  int
			Kind string
			Data json.RawMessage
		}
		if json.Unmarshal([]byte(line), &v) != nil || v.PID != a.PID() || v.Kind != "command" {
			continue
		}
		var cmd struct{ Type, Message string }
		json.Unmarshal(v.Data, &cmd)
		if previous == "clear_queue" && cmd.Type != "prompt" {
			t.Fatal("RPC interleaved inside queue transaction", cmd.Type)
		}
		previous = cmd.Type
		if cmd.Type == "clear_queue" || cmd.Type == "prompt" {
			sequence = append(sequence, cmd.Type)
			if cmd.Type == "prompt" && cmd.Message != "queued\n\nlater\n\ndraft" {
				t.Fatal("queue not merged", cmd.Message)
			}
		}
	}
	if strings.Join(sequence, ",") != "clear_queue,prompt,clear_queue,prompt" {
		t.Fatal("compound operations interleaved", sequence)
	}
	executor, _ := os.FindProcess(a.shared.record.PID)
	executor.Signal(os.Interrupt)
	sharedEventually(t, func() bool { return sharedGone(a.PID()) })
	sharedCall(t, c, map[string]any{"type": "get_state"})
}
