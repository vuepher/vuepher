//go:build !windows

package procman

import (
	"os/exec"
	"syscall"
)

// configureProcess puts the child in its own process group. That is what makes it
// safe to signal the whole group later: `go run .` spawns the compiled binary as
// a grandchild, and signalling only the parent would leave the server holding
// port 8080 (R1).
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stopProcess asks the process group to terminate.
func stopProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid addresses the whole process group.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// killProcess force-kills the process group.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
