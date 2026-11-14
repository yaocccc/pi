//go:build !unix

package runtime

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
