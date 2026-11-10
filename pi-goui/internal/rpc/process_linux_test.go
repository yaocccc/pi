//go:build linux

package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDescendantHelper(t *testing.T) {
	if os.Getenv("GOUI_DESCENDANT_TEST") == "" {
		return
	}
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "-test.run=^TestRPCChild$", "--")
	cmd.Env = append(os.Environ(), "GOUI_RPC_TEST_CHILD=blocked")
	if err := cmd.Start(); err != nil {
		os.Exit(5)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "descendant", "pid": cmd.Process.Pid})
	for {
		time.Sleep(time.Hour)
	}
}
func TestCloseReclaimsDescendants(t *testing.T) {
	t.Setenv("GOUI_DESCENDANT_TEST", "1")
	exe, _ := os.Executable()
	c, err := Start(Config{Bin: exe, Args: []string{"-test.run=^TestDescendantHelper$", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var pid int
	select {
	case event := <-c.Events():
		var v struct{ PID int }
		if err = json.Unmarshal(event.Data, &v); err != nil {
			t.Fatal(err)
		}
		pid = v.PID
	case <-time.After(5 * time.Second):
		t.Fatal("no descendant event")
	}
	if pid <= 0 {
		t.Fatal("invalid descendant PID")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		_, suffix, _ := strings.Cut(string(data), ") ")
		if strings.HasPrefix(suffix, "Z ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("live descendant after Close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
