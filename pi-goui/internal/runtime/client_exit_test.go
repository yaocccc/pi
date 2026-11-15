package runtime

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type locallyClosedPipe struct{}

func (locallyClosedPipe) Read([]byte) (int, error)    { return 0, os.ErrClosed }
func (locallyClosedPipe) Write(p []byte) (int, error) { return len(p), nil }
func (locallyClosedPipe) Close() error                { return nil }

func TestClientLocalReadCloseIsNotForcedShutdown(t *testing.T) {
	for _, closing := range []bool{false, true} {
		t.Run(map[bool]string{false: "unexpected", true: "controlled"}[closing], func(t *testing.T) {
			c := &Client{stdin: locallyClosedPipe{}, stdout: locallyClosedPipe{}, incoming: make(chan Event, 1), done: make(chan struct{}), stop: make(chan struct{}), readDone: make(chan struct{}), stderrDone: make(chan struct{})}
			close(c.done)
			close(c.stderrDone)
			c.closing.Store(closing)
			if closing {
				close(c.stop)
			}
			c.readLoop()
			if c.forcedShutdown.Load() == closing {
				t.Fatalf("closing=%v forced=%v", closing, c.forcedShutdown.Load())
			}
			if !closing && !errors.Is(c.Err(), os.ErrClosed) {
				t.Fatalf("real transport failure lost: %v", c.Err())
			}
		})
	}
}

func TestExitDiagnosticChild(t *testing.T) {
	mode := os.Getenv("GOUI_EXIT_DIAGNOSTIC_CHILD")
	if mode == "" {
		return
	}
	_, _ = io.WriteString(os.Stdout, "{\"type\":\"message_end\"}\n")
	_ = os.Stdout.Close()
	if mode == "hang" {
		for {
			time.Sleep(time.Hour)
		}
	}
	// Closing the protocol output is terminal. Wait for the parent's stdin
	// cleanup before exiting, so stdout EOF always precedes Wait and stderr.
	_, _ = io.Copy(io.Discard, os.Stdin)
	_, _ = io.WriteString(os.Stderr, "fixture final stderr\n")
	os.Exit(7)
}

func TestClientExitDiagnostic(t *testing.T) {
	for _, mode := range []string{"tail", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("GOUI_EXIT_DIAGNOSTIC_CHILD", mode)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			c, err := startDirect(Config{Bin: bin, Args: []string{"-test.run=^TestExitDiagnosticChild$", "--"}, AgentDir: t.TempDir(), NoSession: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			timer := time.NewTimer(6 * time.Second)
			defer timer.Stop()
			var lastErr error
			seenTail := false
			for {
				select {
				case e, ok := <-c.Events():
					if !ok {
						if !seenTail {
							t.Fatal("lost last backend event")
						}
						want := []string{"exit status 7", "fixture final stderr"}
						if mode == "hang" {
							want = []string{"stdout closed before process exit"}
						}
						for _, part := range want {
							if lastErr == nil || !strings.Contains(lastErr.Error(), part) {
								t.Errorf("final event error = %v, want %q", lastErr, part)
							}
							if err := c.Err(); err == nil || !strings.Contains(err.Error(), part) {
								t.Errorf("terminal failure = %v, want %q", err, part)
							}
						}
						select {
						case <-c.done:
						default:
							t.Error("Events closed before backend was reaped")
						}
						return
					}
					if e.Err != nil {
						lastErr = e.Err
					} else if strings.Contains(string(e.Data), "message_end") {
						seenTail = true
					}
				case <-timer.C:
					t.Fatal("client exit did not finish within its cleanup deadline")
				}
			}
		})
	}
}
