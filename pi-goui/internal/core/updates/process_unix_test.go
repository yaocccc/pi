//go:build unix

package updates

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCheckCancellationKillsDescendants(t *testing.T) {
	f := newFixture(t)
	pidFile := filepath.Join(f.cfg.AgentDir, "child.pid")
	put(t, filepath.Join(f.root, "dist/utils/version-check.js"), `
import {spawn} from 'node:child_process';
import {writeFileSync} from 'node:fs';
import {join} from 'node:path';
export async function checkForNewPiVersion(){
 const child=spawn(process.execPath,['--eval','setInterval(()=>{},1000)'],{stdio:'ignore'});
 writeFileSync(join(process.env.PI_CODING_AGENT_DIR,'child.pid'),String(child.pid));
 await new Promise(r=>setTimeout(r,60000));
}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := Check(ctx, f.cfg); done <- err }()
	var pid int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, _ = strconv.Atoi(string(data))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("fixture child did not start")
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("check did not cancel")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		// In containers the orphan may await reaping by PID 1; zombies cannot run.
		if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
			if end := strings.LastIndex(string(data), ")"); end >= 0 && strings.HasPrefix(string(data)[end+1:], " Z") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("checker descendant remained alive")
}
