//go:build windows

package procman

import (
	"os"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether a file is attached to a console.
func isTerminal(file *os.File) bool {
	if file == nil {
		return false
	}
	var mode uint32
	err := windows.GetConsoleMode(windows.Handle(file.Fd()), &mode)
	return err == nil
}
