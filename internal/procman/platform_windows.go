//go:build windows

package procman

import (
	"os/exec"
)

// configureProcess is a no-op on Windows.
//
// Windows has no process-group signal equivalent, so a grandchild spawned by a
// wrapper is not stopped with its parent. This is the documented reason WSL2 is
// the recommended path on Windows (R11): running the toolchain inside WSL2 gets
// the Unix behaviour, including reliable group termination.
//
// TODO(phase-2): use a job object so the child tree can be terminated as a unit
// on native Windows.
func configureProcess(cmd *exec.Cmd) {}

// stopProcess terminates the direct child.
func stopProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// killProcess terminates the direct child.
func killProcess(cmd *exec.Cmd) error { return stopProcess(cmd) }
