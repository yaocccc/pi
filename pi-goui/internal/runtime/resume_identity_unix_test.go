//go:build unix

package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResumeUnwrittenNativePathDoesNotBlockHistory(t *testing.T) {
	for _, closeBlank := range []bool{false, true} {
		t.Run(map[bool]string{false: "blank-stays-open", true: "blank-close-race"}[closeBlank], func(t *testing.T) {
			cfg, root := sharedFixture(t)
			t.Setenv("GOUI_SHARED_ADVERTISE_PATH", "1")
			fresh := cfg
			fresh.SessionFile = ""
			fresh.Args = append([]string(nil), cfg.Args...)
			unwritten := filepath.Join(root, "unwritten.jsonl")
			fresh.Args[len(fresh.Args)-1] = unwritten
			blank, err := startShared(fresh)
			if err != nil {
				t.Fatal(err)
			}
			defer blank.Close()
			if _, err := os.Stat(unwritten); !os.IsNotExist(err) {
				t.Fatal("fixture must retain an unwritten native path:", err)
			}
			// Opening this same absent native pathname must also reuse Pi.
			same := cfg
			same.SessionFile = unwritten
			attached, err := startShared(same)
			if err != nil {
				t.Fatal(err)
			}
			if attached.PID() != blank.PID() {
				t.Fatal("unwritten pathname created a second backend")
			}
			attached.Close()
			var closed chan struct{}
			if closeBlank {
				closed = make(chan struct{})
				go func() { blank.Close(); close(closed) }()
			}
			history, err := startShared(cfg)
			if err != nil {
				t.Fatal("blank session blocked unrelated existing history:", err)
			}
			defer history.Close()
			if closed != nil {
				<-closed
			}
			if history.PID() == blank.PID() {
				t.Fatal("different history reused the blank backend")
			}
			if !closeBlank && sharedGone(blank.PID()) {
				t.Fatal("opening history terminated the blank session")
			}
			wire, err := os.ReadFile(filepath.Join(root, "wire"))
			if err != nil || strings.Count(string(wire), `"kind":"start"`) != 2 {
				t.Fatalf("expected exactly blank + history backends: %s (%v)", wire, err)
			}
			if _, err := os.Stat(unwritten); !os.IsNotExist(err) {
				t.Fatal("discovery wrote the blank session:", err)
			}
		})
	}
}

func TestResumeUnknownIdentityWaitsOnlyForDiscovery(t *testing.T) {
	cfg, root := sharedFixture(t)
	fresh := cfg
	fresh.SessionFile = ""
	blank, err := startShared(fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer blank.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = discoverExecutor(ctx, testSharedRoot(t, cfg.AgentDir), cfg)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("live unknown identity must remain cancellably pending: %v", err)
	}
	dirs, err := os.ReadDir(filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors"))
	if err != nil || len(dirs) != 1 {
		t.Fatal("pending discovery created another executor:", err)
	}
	// Race the actual old-client close with a second Start. Only discovery is
	// retried until the empty executor retires; no operation is replayed.
	closed := make(chan struct{})
	go func() { blank.Close(); close(closed) }()
	history, err := startShared(cfg)
	if err != nil {
		t.Fatal("transient old-executor teardown became fatal:", err)
	}
	defer history.Close()
	<-closed
	wire, _ := os.ReadFile(filepath.Join(root, "wire"))
	if strings.Count(string(wire), `"kind":"start"`) != 2 || strings.Contains(string(wire), `"type":"prompt"`) {
		t.Fatalf("unexpected startup count or replay during discovery: %s", wire)
	}
}

func TestResumeConfirmedPathDoesNotBypassUnknownOwnership(t *testing.T) {
	for _, state := range []string{"unknown-start", "orphan", "different-absent-target", "unsafe-native-path"} {
		t.Run(state, func(t *testing.T) {
			cfg, root := sharedFixture(t)
			slot := ownershipRoot(t, Config{SessionFile: filepath.Join(root, "unwritten.jsonl"), AgentDir: cfg.AgentDir})
			native := filepath.Join(root, "unwritten.jsonl")
			r := sharedRecord{Socket: filepath.Join(slot, "rpc.sock"), Token: "fixture", PID: os.Getpid(), Birth: sharedProcessBirth(os.Getpid()), Keys: []string{"path:" + native}, IdentityPending: true, NativePath: native}
			switch state {
			case "unknown-start":
				r.Starting = true
			case "orphan":
				r.PID, r.Birth = os.Getppid(), "obsolete-fixture-birth"
				r.Backends = []int{os.Getpid()}
				r.BackendBirth = map[int]string{os.Getpid(): sharedProcessBirth(os.Getpid())}
			case "different-absent-target":
				cfg.SessionFile = filepath.Join(root, "other-absent.jsonl")
			case "unsafe-native-path":
				if err := os.Symlink(cfg.SessionFile, native); err != nil {
					t.Fatal(err)
				}
			}
			if err := publishSharedRecord(slot, r); err != nil {
				t.Fatal(err)
			}
			_, err := resolveExecutor(testSharedRoot(t, cfg.AgentDir), cfg)
			if err == nil || !strings.Contains(err.Error(), "identity pending") {
				t.Fatal("confirmed path bypassed uncertain ownership:", err)
			}
			if state == "orphan" && errors.Is(err, errExecutorIdentityPending) {
				t.Fatal("orphan incorrectly classified as retryable discovery")
			}
			dirs, err := os.ReadDir(filepath.Join(testSharedRoot(t, cfg.AgentDir), "executors"))
			if err != nil || len(dirs) != 1 {
				t.Fatal("uncertain ownership created another executor:", err)
			}
		})
	}
}
