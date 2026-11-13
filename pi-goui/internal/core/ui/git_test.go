package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestGitStatusParse(t *testing.T) {
	raw := "## feature/topic...origin/feature/topic [ahead 2]\x00 M unstaged\x00M  staged\x00MM both\x00?? new\nfile\x00UU conflict\x00R  renamed\x00 M source-that-is-not-a-status\x00 C copy\x00?? old-copy-source\x00!! ignored\x00"
	got := parseGitStatus([]byte(raw), false)
	if got == nil || got.Branch != "feature/topic" || got.Staged != 3 || got.Modified != 3 || got.Untracked != 1 || got.Conflicts != 1 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	for _, header := range []string{"No commits yet on main", "Initial commit on main"} {
		if g := parseGitStatus([]byte("## "+header+"\x00"), false); g == nil || g.Branch != "main" {
			t.Fatalf("unborn branch: %+v", g)
		}
	}
	if g := parseGitStatus([]byte("## HEAD (no branch)\x00"), false); g == nil || g.Branch != "HEAD (detached)" {
		t.Fatalf("detached branch: %+v", g)
	}
	cut := parseGitStatus([]byte("## main\x00?? complete\x00?? incomplete"), true)
	if cut == nil || !cut.Truncated || cut.Untracked != 1 {
		t.Fatalf("incomplete record counted: %+v", cut)
	}
	if parseGitStatus([]byte("garbage\x00"), false) != nil || parseGitStatus([]byte("## partial"), true) != nil {
		t.Fatal("invalid header accepted")
	}
}
func TestGitOutputCapDrains(t *testing.T) {
	b := &cappedGitOutput{limit: 7}
	if n, e := b.Write([]byte(strings.Repeat("x", 100))); n != 100 || e != nil {
		t.Fatalf("write: %d %v", n, e)
	}
	if n, e := b.Write([]byte("more")); n != 4 || e != nil {
		t.Fatal("must keep draining")
	}
	if b.buf.Len() != 7 || !b.truncated {
		t.Fatal("output cap not enforced")
	}
}
func fakeGit(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if e := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir)
	return dir
}
func TestGitCommandFlagsEnvironmentAndDetached(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	t.Setenv("GOUI_GIT_LOG", log)
	t.Setenv("GIT_DIR", "/must-not-use")
	t.Setenv("GIT_WORK_TREE", "/must-not-use")
	t.Setenv("GIT_INDEX_FILE", "/must-not-write")
	fakeGit(t, `[ -z "$GIT_DIR$GIT_WORK_TREE$GIT_INDEX_FILE" ] || exit 90
[ "$GIT_OPTIONAL_LOCKS" = 0 ] || exit 91
printf '%s\n' "$*" >> "$GOUI_GIT_LOG"
case "$*" in
 *rev-parse*) printf 'abcd1234\n';;
 *) printf '## HEAD (no branch)\000 M file\000';;
esac`)
	var r gitReader
	defer r.close()
	msg := r.command("/specific/cwd")().(gitStatusMsg)
	if msg.err != nil || msg.cwd != "/specific/cwd" || msg.info == nil || msg.info.Branch != "detached abcd1234" || msg.info.Modified != 1 {
		t.Fatalf("result: %+v info=%+v", msg, msg.info)
	}
	data, e := os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	calls := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(calls) != 2 {
		t.Fatalf("calls: %q", calls)
	}
	for _, call := range calls {
		for _, flag := range []string{"--no-optional-locks", "-c core.fsmonitor=false", "-C /specific/cwd"} {
			if !strings.Contains(call, flag) {
				t.Fatalf("missing %s in %s", flag, call)
			}
		}
	}
	if !strings.Contains(calls[0], "status --porcelain=v1 --branch -z --untracked-files=normal") {
		t.Fatal("unsafe status command")
	}
}
func TestGitCommandLargeOutputBounded(t *testing.T) {
	fakeGit(t, `printf '## main\000'
i=0
while [ "$i" -lt 10000 ]; do printf '?? abcdefghijklmnopqrstuvwxyz-abcdefghijklmnopqrstuvwxyz\000'; i=$((i+1)); done`)
	var r gitReader
	defer r.close()
	msg := r.command(t.TempDir())().(gitStatusMsg)
	if msg.err != nil || msg.info == nil || !msg.info.Truncated || msg.info.Untracked == 0 || msg.info.Untracked >= 10000 {
		t.Fatalf("large output: %+v %+v", msg, msg.info)
	}
}
func TestGitCloseBoundedAndNoLaterSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process checks")
	}
	sleep, e := exec.LookPath("sleep")
	if e != nil {
		t.Skip("sleep unavailable")
	}
	marker := filepath.Join(t.TempDir(), "pid")
	t.Setenv("GOUI_GIT_PID", marker)
	fakeGit(t, fmt.Sprintf("printf '%%s' \"$$\" > \"$GOUI_GIT_PID\"\nexec %q 30", sleep))
	var r gitReader
	messages := make(chan gitStatusMsg, 1)
	start := time.Now()
	go func() { messages <- r.command(t.TempDir())().(gitStatusMsg) }()
	deadline := time.Now().Add(time.Second)
	var pid int
	for time.Now().Before(deadline) {
		if data, e := os.ReadFile(marker); e == nil {
			pid, _ = strconv.Atoi(string(data))
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid == 0 {
		r.close()
		t.Fatal("child did not start")
	}
	r.close()
	if time.Since(start) > 3*time.Second {
		t.Fatal("close exceeded bounded query deadline")
	}
	msg := <-messages
	if !errors.Is(msg.err, context.DeadlineExceeded) {
		t.Fatalf("timeout error: %v", msg.err)
	}
	process, _ := os.FindProcess(pid)
	if e := process.Signal(syscall.Signal(0)); e == nil {
		t.Fatalf("git child %d survived close", pid)
	}
	if e := os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	closed := r.command("unused")().(gitStatusMsg)
	if closed.info != nil || closed.err != nil {
		t.Fatalf("closed query: %+v", closed)
	}
	if _, e := os.Stat(marker); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("closed helper spawned another child")
	}
}
func TestGitRealReadOnlyUnbornAndNonRepo(t *testing.T) {
	git, e := exec.LookPath("git")
	if e != nil {
		t.Skip("git unavailable")
	}
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	repo := filepath.Join(root, "repo")
	cmd := exec.Command(git, "init", "--template=", "--initial-branch=main", repo)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1")
	if data, e := cmd.CombinedOutput(); e != nil {
		t.Skipf("git init unavailable: %v %s", e, data)
	}
	if e := os.WriteFile(filepath.Join(repo, "untracked\nfile"), []byte("example"), 0600); e != nil {
		t.Fatal(e)
	}
	configPath := filepath.Join(repo, ".git", "config")
	before, e := os.ReadFile(configPath)
	if e != nil {
		t.Fatal(e)
	}
	var r gitReader
	defer r.close()
	got := r.command(repo)().(gitStatusMsg)
	if got.err != nil || got.info == nil || got.info.Branch != "main" || got.info.Untracked != 1 {
		t.Fatalf("real git status: %+v %+v", got, got.info)
	}
	after, e := os.ReadFile(configPath)
	if e != nil || string(before) != string(after) {
		t.Fatal("Git config changed")
	}
	if _, e := os.Stat(filepath.Join(repo, ".git", "index")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("status unexpectedly created an index")
	}
	if msg := r.command(home)().(gitStatusMsg); msg.info != nil {
		t.Fatalf("non-repository returned metadata: %+v", msg.info)
	}
}
func TestGitMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var r gitReader
	defer r.close()
	if msg := r.command(t.TempDir())().(gitStatusMsg); msg.info != nil || msg.err == nil {
		t.Fatalf("missing Git: %+v", msg)
	}
}
