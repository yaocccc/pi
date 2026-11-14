//go:build unix

package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSharedConcurrentStartsAndHardLinkIdentity(t *testing.T) {
	cfg, _ := sharedFixture(t)
	const count = 8
	clients := make(chan *Client, count)
	fail := make(chan error, count)
	var wg sync.WaitGroup
	for range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := startShared(cfg)
			if e != nil {
				fail <- e
			} else {
				clients <- c
			}
		}()
	}
	wg.Wait()
	close(clients)
	close(fail)
	for e := range fail {
		t.Error(e)
	}
	var all []*Client
	pid := 0
	for c := range clients {
		all = append(all, c)
		if pid == 0 {
			pid = c.PID()
		}
		if c.PID() != pid {
			t.Error("concurrent Start duplicated writer")
		}
		defer c.Close()
	}
	if len(all) != count {
		t.Fatal("not all clients attached")
	}
	alias := filepath.Join(filepath.Dir(cfg.SessionFile), "hard-link.jsonl")
	if e := os.Link(cfg.SessionFile, alias); e != nil {
		t.Skip(e)
	}
	cfg.SessionFile = alias
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if c.PID() != pid {
		t.Fatal("hard link duplicated session")
	}
	c.Close()
	for _, c := range all {
		c.Close()
	}
	sharedEventually(t, func() bool { return sharedGone(pid) })
}
func TestSharedRejectsUntrustedRegistrationAndSessionLinks(t *testing.T) {
	cfg, root := sharedFixture(t)
	real := cfg.SessionFile
	cfg.SessionFile = filepath.Join(root, "link.jsonl")
	if e := os.Symlink(real, cfg.SessionFile); e != nil {
		t.Fatal(e)
	}
	if c, e := startShared(cfg); e == nil {
		c.Close()
		t.Fatal("symlink session accepted")
	}
	registry, e := sharedRoot(filepath.Join(root, "untrusted"))
	if e != nil {
		t.Fatal(e)
	}
	secret := filepath.Join(root, "other")
	if e = os.WriteFile(secret, []byte("private data"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(secret, filepath.Join(registry, "endpoint.json")); e != nil {
		t.Fatal(e)
	}
	if _, e = ensureSharedBroker(registry); e == nil {
		t.Fatal("symlink registration accepted")
	}
	if e = os.Chmod(registry, 0755); e != nil {
		t.Fatal(e)
	}
	if _, e = sharedRoot(filepath.Join(root, "untrusted")); e == nil {
		t.Fatal("public registry accepted")
	}
}
func TestSharedMutationContextCancellationReleasesLease(t *testing.T) {
	cfg, _ := sharedFixture(t)
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	release, e := c.BeginFileMutation(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	c.Close()
	cancel()
	var next *Client
	sharedEventually(t, func() bool { next, e = startShared(cfg); return e == nil })
	next.Close()
}
func TestSharedLastClientReclaimsIndependentTools(t *testing.T) {
	cfg, _ := sharedFixture(t)
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	sharedCall(t, c, map[string]any{"type": "spawn-independent-tool"})
	var event struct{ PID int }
	if e = json.Unmarshal(sharedEvent(t, c, "fixture_tool"), &event); e != nil || event.PID <= 0 {
		t.Fatal("missing tool PID", e)
	}
	pid := c.PID()
	c.Close()
	sharedEventually(t, func() bool { return sharedGone(pid) && sharedGone(event.PID) })
}

func TestSharedBrokerSignalReclaimsBackend(t *testing.T) {
	cfg, _ := sharedFixture(t)
	c, e := startShared(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	pid := c.PID()
	process, e := os.FindProcess(c.shared.record.PID)
	if e != nil {
		t.Fatal(e)
	}
	if e = process.Signal(os.Interrupt); e != nil {
		t.Fatal(e)
	}
	sharedEventually(t, func() bool { return sharedGone(pid) })
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("broker shutdown did not release client")
	}
}
