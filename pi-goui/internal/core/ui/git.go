package ui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"pi-goui/internal/core/render"
)

const gitQueryTimeout = 2 * time.Second
const gitOutputLimit = 256 << 10

type gitStatusMsg struct {
	cwd  string
	info *render.GitStatus
	err  error
}

// A query owns the lock until its bounded child has been reaped. Close waits for
// that query and prevents commands queued by Bubble Tea from starting later.
type gitReader struct {
	mu     sync.Mutex
	closed atomic.Bool
}

func (r *gitReader) close() {
	r.closed.Store(true)
	r.mu.Lock()
	r.mu.Unlock()
}
func (r *gitReader) command(cwd string) tea.Cmd {
	return func() tea.Msg {
		r.mu.Lock()
		defer r.mu.Unlock()
		result := gitStatusMsg{cwd: cwd}
		if r.closed.Load() {
			return result
		}
		ctx, cancel := context.WithTimeout(context.Background(), gitQueryTimeout)
		defer cancel()
		data, truncated, err := gitQuery(ctx, cwd, "status", "--porcelain=v1", "--branch", "-z", "--untracked-files=normal")
		if err != nil {
			result.err = err
			return result
		}
		result.info = parseGitStatus(data, truncated)
		if result.info != nil && result.info.Branch == "HEAD (detached)" {
			hash, cut, e := gitQuery(ctx, cwd, "rev-parse", "--short=8", "HEAD")
			if e == nil && !cut {
				short := strings.TrimSpace(string(hash))
				if isGitHash(short) {
					result.info.Branch = "detached " + short
				}
			}
		}
		return result
	}
}

// Keep draining after the cap: a large status result cannot deadlock Git or
// grow memory without bound. Only complete NUL records are parsed below.
type cappedGitOutput struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedGitOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := max(0, b.limit-b.buf.Len())
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return n, nil
}
func gitQuery(ctx context.Context, cwd string, args ...string) ([]byte, bool, error) {
	argv := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "gc.auto=0", "-C", cwd}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	// Parent repository overrides must not redirect a query away from cwd.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	stdout := &cappedGitOutput{limit: gitOutputLimit}
	stderr := &cappedGitOutput{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 250 * time.Millisecond
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		// Do not include stderr: it may contain private paths or user Git settings.
		return nil, false, err
	}
	return stdout.buf.Bytes(), stdout.truncated, nil
}
func isGitHash(s string) bool {
	if len(s) < 4 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func parseGitStatus(data []byte, truncated bool) *render.GitStatus {
	records := bytes.Split(data, []byte{0})
	// The final item is either empty or a capped, incomplete record.
	if len(records) < 2 {
		return nil
	}
	records = records[:len(records)-1]
	header := string(records[0])
	if !strings.HasPrefix(header, "## ") {
		return nil
	}
	branch := strings.TrimPrefix(header, "## ")
	for _, prefix := range []string{"No commits yet on ", "Initial commit on "} {
		branch = strings.TrimPrefix(branch, prefix)
	}
	if branch == "HEAD (no branch)" {
		branch = "HEAD (detached)"
	} else {
		branch, _, _ = strings.Cut(branch, "...")
	}
	info := &render.GitStatus{Branch: branch, Truncated: truncated}
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 3 || record[2] != ' ' {
			continue
		}
		x, y := record[0], record[1]
		switch string(record[:2]) {
		case "??":
			info.Untracked++
		case "!!":
			continue
		case "DD", "AU", "UD", "UA", "DU", "AA", "UU":
			info.Conflicts++
		default:
			if x != ' ' {
				info.Staged++
			}
			if y != ' ' {
				info.Modified++
			}
		}
		// In porcelain v1 -z, rename/copy destination precedes its source path.
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
		}
	}
	return info
}
