//go:build !unix

package web

import (
	"errors"
	"os"
	"os/exec"
)

func startPTY(*exec.Cmd, int, int) (*os.File, error) {
	return nil, errors.New("browser TUI requires a Unix PTY; on Windows use WSL")
}
func resizePTY(*os.File, int, int) error { return errors.New("PTY unavailable on this platform") }
func terminatePTY(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
func killPTY(cmd *exec.Cmd)              { terminatePTY(cmd) }
func preparePTYCleanup(*exec.Cmd) func() { return func() {} }
