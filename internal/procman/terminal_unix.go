//go:build !windows

package procman

import (
	"os"

	"golang.org/x/sys/unix"
)

// isTerminal reports whether a file is attached to a terminal.
func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	_, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	return err == nil
}
