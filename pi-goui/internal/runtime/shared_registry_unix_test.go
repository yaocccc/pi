//go:build unix

package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedRegistryHomeNamespace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{filepath.Join(home, ".pi", "agent"), t.TempDir()} {
		root := testSharedRoot(t, agent)
		canonical, err := CanonicalAgentDir(agent)
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(canonical))
		want := filepath.Join(canonicalHome, ".pi", "goui-rpc", hex.EncodeToString(hash[:]))
		if root != want {
			t.Fatalf("root = %q, want %q", root, want)
		}
		for _, dir := range []string{filepath.Dir(root), root} {
			info, err := os.Stat(dir)
			if err != nil || info.Mode().Perm() != 0700 {
				t.Fatalf("registry not private: %s, %v", dir, err)
			}
		}
		if _, err := os.Lstat(filepath.Join(agent, "goui-rpc")); !os.IsNotExist(err) {
			t.Fatalf("created legacy state: %v", err)
		}
		alias := filepath.Join(t.TempDir(), "agent-alias")
		if err := os.Symlink(agent, alias); err != nil {
			t.Fatal(err)
		}
		if got := testSharedRoot(t, alias); got != root {
			t.Fatal("agent alias split the registry", got, root)
		}
		if other := testSharedRoot(t, t.TempDir()); other == root {
			t.Fatal("different agents share a registry")
		}
	}
	alias := filepath.Join(t.TempDir(), "home-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	agent := t.TempDir()
	root := testSharedRoot(t, agent)
	t.Setenv("HOME", alias)
	if got := testSharedRoot(t, agent); got != root {
		t.Fatal("HOME alias split the registry")
	}
}

func TestSharedRegistryLegacyRefusedWithoutChanges(t *testing.T) {
	for _, kind := range []string{"empty", "records", "symlink", "file"} {
		t.Run(kind, func(t *testing.T) {
			home, agent := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			legacy := filepath.Join(agent, "goui-rpc")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(t.TempDir(), legacy)
			case "file":
				err = os.WriteFile(legacy, []byte("do not touch"), 0600)
			default:
				err = os.Mkdir(legacy, 0700)
				if err == nil && kind == "records" {
					err = os.WriteFile(filepath.Join(legacy, "endpoint.json"), []byte("unknown writer"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.Lstat(legacy)
			if _, err = sharedRoot(agent); err == nil || !strings.Contains(err.Error(), "stop old instances") {
				t.Fatal("legacy registry was not safely refused", err)
			}
			if member, err := JoinSessionMember(agent); err == nil {
				member.Close()
				t.Fatal("joined despite legacy registry")
			}
			after, err := os.Lstat(legacy)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
				t.Fatal("legacy registry changed", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".pi", "goui-rpc")); !os.IsNotExist(err) {
				t.Fatal("legacy refusal created a new registry", err)
			}
			if kind == "records" {
				data, err := os.ReadFile(filepath.Join(legacy, "endpoint.json"))
				entries, listErr := os.ReadDir(legacy)
				if err != nil || string(data) != "unknown writer" || listErr != nil || len(entries) != 1 {
					t.Fatal("legacy records changed")
				}
			}
		})
	}
}

func TestSharedRegistryRejectsUnsafePaths(t *testing.T) {
	for _, kind := range []string{"empty-home", "relative-home", "missing-home", "file-home", "writable-home", "parent-link", "parent-writable", "base-link", "base-public", "namespace-link", "namespace-public"} {
		t.Run(kind, func(t *testing.T) {
			home, agent := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			parent, base := filepath.Join(home, ".pi"), filepath.Join(home, ".pi", "goui-rpc")
			if err := os.MkdirAll(base, 0700); err != nil {
				t.Fatal(err)
			}
			canonicalAgent, err := CanonicalAgentDir(agent)
			if err != nil {
				t.Fatal(err)
			}
			namespace := filepath.Join(base, sharedAgentHash(canonicalAgent))
			switch kind {
			case "empty-home":
				t.Setenv("HOME", "")
			case "relative-home":
				t.Setenv("HOME", "relative")
			case "missing-home":
				t.Setenv("HOME", filepath.Join(home, "missing"))
			case "file-home":
				file := filepath.Join(home, "file")
				err = os.WriteFile(file, nil, 0600)
				t.Setenv("HOME", file)
			case "writable-home":
				err = os.Chmod(home, 0777)
			case "parent-link":
				// The target and the disposable source tree are test-owned.
				err = os.Rename(parent, filepath.Join(home, "pi-target"))
				if err == nil {
					err = os.Symlink(filepath.Join(home, "pi-target"), parent)
				}
			case "parent-writable":
				err = os.Chmod(parent, 0777)
			case "base-link":
				err = os.Remove(base)
				if err == nil {
					err = os.Symlink(t.TempDir(), base)
				}
			case "base-public":
				err = os.Chmod(base, 0755)
			case "namespace-link":
				err = os.Symlink(t.TempDir(), namespace)
			case "namespace-public":
				err = os.Mkdir(namespace, 0755)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sharedRoot(agent); err == nil {
				t.Fatal("unsafe shared registry accepted")
			}
		})
	}
}

func TestExecutorRegistryAgentBinding(t *testing.T) {
	agent := t.TempDir()
	root := filepath.Join(testSharedRoot(t, agent), "executors", strings.Repeat("a", 64))
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	canonicalAgent, err := CanonicalAgentDir(agent)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := executorAgent(root, agent); err != nil || got != canonicalAgent {
		t.Fatal("valid agent rejected", got, err)
	}
	if _, err := executorAgent(root, t.TempDir()); err == nil {
		t.Fatal("another agent attached to the namespace")
	}
	for _, bad := range []string{filepath.Join(t.TempDir(), "goui-rpc", "executors", strings.Repeat("a", 64)), root + "/.", filepath.Join(filepath.Dir(root), strings.Repeat("A", 64))} {
		if handled, err := RunSharedSessionHelper([]string{sharedServe, bad}); !handled || err == nil {
			t.Fatal("helper accepted registry outside its canonical namespace", bad, err)
		}
	}
	if err := os.Mkdir(filepath.Join(agent, "goui-rpc"), 0700); err != nil {
		t.Fatal(err)
	}
	b := &sessionBroker{root: root}
	if _, _, err := b.attach(Config{AgentDir: agent}, nil); err == nil || !strings.Contains(err.Error(), "legacy shared RPC registry") {
		t.Fatal("attach failed to recheck legacy state", err)
	}
}

func TestSharedRegistryPreservesBackendAgentAndAliases(t *testing.T) {
	cfg, _ := sharedFixture(t)
	alias := filepath.Join(t.TempDir(), "agent-alias")
	if err := os.Symlink(cfg.AgentDir, alias); err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalAgentDir(cfg.AgentDir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	cfg.AgentDir = alias
	second, err := startShared(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.PID() != second.PID() {
		t.Fatal("agent alias launched another Pi")
	}
	var result struct{ AgentDir string }
	if err := json.Unmarshal(sharedCall(t, first, map[string]any{"type": "get_fixture_agent"}), &result); err != nil || result.AgentDir != canonical {
		t.Fatal("backend received registry path instead of actual agent", result.AgentDir, err)
	}
	conn, err := sharedDial(first.shared.record)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	wrong := cfg
	wrong.AgentDir = t.TempDir()
	if _, err := sharedHandshake(conn, sharedHello{Token: first.shared.record.Token, Action: "attach", Config: wrong}); err == nil || !strings.Contains(err.Error(), "another agent") {
		t.Fatal("real transport accepted a different agent", err)
	}
	if err := os.Mkdir(filepath.Join(canonical, "goui-rpc"), 0700); err != nil {
		t.Fatal(err)
	}
	if client, err := startShared(cfg); err == nil {
		client.Close()
		t.Fatal("new client ignored a later legacy registry")
	}
	// Existing clients are not killed or migrated by a failed admission.
	if err := json.Unmarshal(sharedCall(t, first, map[string]any{"type": "get_fixture_agent"}), &result); err != nil || result.AgentDir != canonical {
		t.Fatal("legacy refusal disturbed the existing backend", err)
	}
}
