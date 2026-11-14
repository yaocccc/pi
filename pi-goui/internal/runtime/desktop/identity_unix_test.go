//go:build unix

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pi-goui/internal/runtime"
)

func TestDesktopCanonicalAgentScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agent := t.TempDir()
	canonical, err := runtime.CanonicalAgentDir(agent)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "agent-alias")
	if err := os.Symlink(agent, alias); err != nil {
		t.Fatal(err)
	}
	endpoint, err := OpenDesktop(alias, "private-agent-token")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	file := filepath.Join(os.Getenv("HOME"), ".pi", desktopRegistry, endpoint.ID+".json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var stored Record
	if err := json.Unmarshal(data, &stored); err != nil || stored.AgentDir != canonical {
		t.Fatalf("registration did not canonicalize agent: %q %v", stored.AgentDir, err)
	}
	for _, scope := range []string{agent, alias, filepath.Join(alias, ".")} {
		record, ok := ReadDesktopRecord(scope, endpoint.ID)
		if !ok || record.AgentDir != canonical || len(ReadDesktopRecords(scope)) != 1 {
			t.Fatalf("same-agent alias not discovered: %s", scope)
		}
	}
	other := t.TempDir()
	if _, ok := ReadDesktopRecord(other, endpoint.ID); ok || len(ReadDesktopRecords(other)) != 0 {
		t.Fatal("agent scopes mixed")
	}
	// Existing records containing an absolute alias remain readable. Discovery
	// normalizes in memory only and must not rewrite another owner's record.
	stored.AgentDir = alias
	data, err = json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if record, ok := ReadDesktopRecord(agent, endpoint.ID); !ok || record.AgentDir != canonical {
		t.Fatal("existing alias registration was rejected")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != string(data) {
		t.Fatal("discovery modified an existing registration", err)
	}
	stored.AgentDir = "."
	data, _ = json.Marshal(stored)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadDesktopRecord(agent, endpoint.ID); ok {
		t.Fatal("relative stored agent accepted")
	}
	// This test replaced the descriptor; Close must leave that replacement.
	t.Cleanup(func() { _ = os.Remove(file) })
}

func TestDesktopFirstStartupAndEmptyAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := OpenDesktop("", "private-token"); err == nil {
		t.Fatal("empty agent accepted")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".pi")); !os.IsNotExist(err) {
		t.Fatal("invalid open touched registry", err)
	}
	agent := filepath.Join(t.TempDir(), "new-agent")
	if len(ReadDesktopRecords(agent)) != 0 {
		t.Fatal("missing agent returned records")
	}
	if _, err := os.Stat(agent); !os.IsNotExist(err) {
		t.Fatal("discovery created agent directory", err)
	}
	endpoint, err := OpenDesktop(agent, "private-token")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Close()
	if _, ok := ReadDesktopRecord(agent, endpoint.ID); !ok {
		t.Fatal("first startup registration missing")
	}
	if _, err := os.Stat(filepath.Join(agent, "goui-rpc")); !os.IsNotExist(err) {
		t.Fatal("desktop endpoint created election membership", err)
	}
}
