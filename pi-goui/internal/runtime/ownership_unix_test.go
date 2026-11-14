//go:build unix

package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnershipPendingIdentityBeforeAliasPublication(t *testing.T) {
	for _, newSession := range []bool{true, false} {
		t.Run(map[bool]string{true: "new-session", false: "nonexistent-path-hardlink"}[newSession], func(t *testing.T) {
			cfg, root := sharedFixture(t)
			file := cfg.SessionFile
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if newSession {
				cfg.SessionFile = ""
			}
			a, err := startShared(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			go func() {
				for range a.Events() {
				}
			}()
			release := filepath.Join(root, "wire.release")
			defer os.WriteFile(release, nil, 0600)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := a.Call(ctx, map[string]any{"type": "prompt", "message": "ownership-create-pause"})
				done <- err
			}()
			sharedEventually(t, func() bool { _, err := os.Stat(filepath.Join(root, "wire.created")); return err == nil })
			resume := cfg
			resume.SessionFile = file
			if !newSession {
				resume.SessionFile = filepath.Join(root, "alias.jsonl")
				if err := os.Link(file, resume.SessionFile); err != nil {
					t.Fatal(err)
				}
			}
			registry := testSharedRoot(t, cfg.AgentDir)
			dirs, _ := os.ReadDir(filepath.Join(registry, "executors"))
			if newSession {
				if _, err := resolveExecutor(registry, resume); err == nil || !strings.Contains(err.Error(), "identity pending") {
					t.Fatalf("unknown native path attached before identity publication: %v", err)
				}
			} else {
				// The confirmed native path can be re-statted while Pi is paused,
				// before its inode alias is published. Discovery must reuse it,
				// and attaching must not need another blocked get_state RPC.
				b, err := startShared(resume)
				if err != nil {
					t.Fatal("confirmed native hard-link alias did not attach:", err)
				}
				defer b.Close()
				if b.PID() != a.PID() {
					t.Fatal("hard-link discovery started a second writer")
				}
			}
			after, _ := os.ReadDir(filepath.Join(registry, "executors"))
			if len(after) != len(dirs) {
				t.Fatal("pending identity created another executor")
			}
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			sharedEventually(t, func() bool {
				r, err := readSharedRecord(filepath.Join(registry, "executors", dirs[0].Name()))
				return err == nil && !r.IdentityPending
			})
			b, err := startShared(resume)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			if b.PID() != a.PID() {
				t.Fatal("published identity did not reuse original Pi")
			}
		})
	}
}

func ownershipRoot(t *testing.T, cfg Config) string {
	t.Helper()
	key, _, err := sharedSessionIdentity(cfg.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(key))
	root := filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors", hex.EncodeToString(hash[:]))
	if err := privateDirectory(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOwnershipEmptyBootstrapRecoveryAndGenerationFence(t *testing.T) {
	cfg, _ := sharedFixture(t)
	root := ownershipRoot(t, cfg)
	lock, err := lockSharedRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := lock.Stat()
	unlockSharedRoot(lock)
	// Exactly the crash after mkdir and before launcher/reservation.
	c, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	after, err := os.Stat(filepath.Join(root, "lock"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("bootstrap recovery replaced lock inode")
	}
	// A delayed launcher must not overwrite a running executor or its tombstone.
	if err := serveShared(root, "obsolete-generation"); err == nil {
		t.Fatal("stale launcher admitted")
	}
	c.Close()
	sharedEventually(t, func() bool { return sharedGone(c.PID()) })
}

func TestOwnershipReservedBootstrapRetriesWithoutOldLauncher(t *testing.T) {
	cfg, _ := sharedFixture(t)
	root := ownershipRoot(t, cfg)
	key, _, _ := sharedSessionIdentity(cfg.SessionFile)
	old := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: "old-generation", PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Keys: []string{key}, Bootstrap: true}
	if err := publishSharedRecord(root, old); err != nil {
		t.Fatal(err)
	}
	if err := markExecutorLaunched(root); err != nil {
		t.Fatal(err)
	}
	c, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := serveShared(root, old.Token); err == nil {
		t.Fatal("late previous launcher admitted")
	}
}

func TestOwnershipAbandonedNewReservationDoesNotSealAgent(t *testing.T) {
	cfg, _ := sharedFixture(t)
	hash := sha256.Sum256([]byte("new:abandoned"))
	root := filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors", hex.EncodeToString(hash[:]))
	if err := privateDirectory(root); err != nil {
		t.Fatal(err)
	}
	r := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: "delayed-launcher", PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Keys: []string{"new:abandoned"}, Bootstrap: true, IdentityPending: true}
	if err := publishSharedRecord(root, r); err != nil {
		t.Fatal(err)
	}
	if err := markExecutorLaunched(root); err != nil {
		t.Fatal(err)
	}
	c, err := startShared(cfg)
	if err != nil {
		t.Fatal("abandoned launcher blocked unrelated session:", err)
	}
	defer c.Close()
	after, err := readSharedRecord(root)
	if err != nil || !after.Retired || after.Bootstrap {
		t.Fatal("unstarted reservation was not retired")
	}
	if err := serveShared(root, r.Token); err == nil {
		t.Fatal("retired delayed helper admitted")
	}
}

func TestOwnershipStaleGenerationCannotPublishAfterOwnerUnlock(t *testing.T) {
	cfg, _ := sharedFixture(t)
	root := ownershipRoot(t, cfg)
	for _, bootstrap := range []bool{true, false} {
		r := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: "current-generation", PID: os.Getpid(), Bootstrap: bootstrap}
		if err := publishSharedRecord(root, r); err != nil {
			t.Fatal(err)
		}
		if err := serveShared(root, "obsolete-generation"); err == nil || !strings.Contains(err.Error(), "stale") {
			t.Fatalf("unlocked stale helper was not fenced: %v", err)
		}
		after, err := readSharedRecord(root)
		if err != nil || after.Token != r.Token || after.Bootstrap != bootstrap {
			t.Fatal("stale helper overwrote the reservation/tombstone")
		}
	}
}

func TestOwnershipPendingIdentityAllowsKnownAndExplicitNew(t *testing.T) {
	cfg, _ := sharedFixture(t)
	known, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer known.Close()
	fresh := cfg
	fresh.SessionFile = ""
	pending, err := startShared(fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	other, err := startShared(cfg)
	if err != nil {
		t.Fatal("pending identity blocked known session:", err)
	}
	defer other.Close()
	if other.PID() != known.PID() {
		t.Fatal("known session was not reused")
	}
	another, err := startShared(fresh)
	if err != nil {
		t.Fatal("pending identity blocked explicit new session:", err)
	}
	defer another.Close()
	if another.PID() == pending.PID() {
		t.Fatal("explicit new reused an unrelated pending session")
	}
}

func TestOwnershipMissingMetadataIsNotEmptyBootstrap(t *testing.T) {
	for _, evidence := range []string{"launched", "owner"} {
		t.Run(evidence, func(t *testing.T) {
			cfg, _ := sharedFixture(t)
			root := ownershipRoot(t, cfg)
			if evidence == "launched" {
				if err := markExecutorLaunched(root); err != nil {
					t.Fatal(err)
				}
			} else if err := privateDirectory(filepath.Join(root, "owner")); err != nil {
				t.Fatal(err)
			}
			if _, err := resolveExecutor(testSharedRoot(t, cfg.AgentDir), cfg); err == nil {
				t.Fatal("unknown prior writer mistaken for empty bootstrap")
			}
			if _, err := ensureSharedBroker(root); err == nil {
				t.Fatal("direct bootstrap bypassed ownership fence")
			}
		})
	}
}

func TestOwnershipUnknownStartCannotReplaceOrphanBackend(t *testing.T) {
	cfg, _ := sharedFixture(t)
	root := ownershipRoot(t, cfg)
	key, _, _ := sharedSessionIdentity(cfg.SessionFile)
	// Reconstruct the exact durable state at executor death between cmd.Start
	// and PID publication: no known PID, but a write-ahead uncertainty record.
	r := sharedRecord{Socket: filepath.Join(root, "rpc.sock"), Token: "crash-window", PID: os.Getpid(), Birth: "dead-owner-birth", Keys: []string{key}, Starting: true}
	if err := publishSharedRecord(root, r); err != nil {
		t.Fatal(err)
	}
	backend, err := startDirect(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if _, err := ensureSharedBroker(root, key); err == nil || !strings.Contains(err.Error(), "start outcome unknown") {
		t.Fatalf("orphan start window was replaced: %v", err)
	}
	if sharedGone(backend.PID()) {
		t.Fatal("fixture backend must remain alive during recovery attempt")
	}
	after, err := readSharedRecord(root)
	if err != nil || !after.Starting || len(after.Backends) != 0 {
		t.Fatal("recovery erased uncertainty")
	}
}
