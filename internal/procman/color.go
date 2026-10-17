package procman

import (
	"os"
	"strings"
)

// noColor reports whether colour output should be suppressed, honouring the
// NO_COLOR convention (https://no-color.org) and a non-interactive terminal.
//
// Colour is never emitted when the output is redirected, so piping `vuepher dev`
// into a file produces readable logs rather than escape sequences.
func noColor() bool {
	if value, ok := os.LookupEnv("NO_COLOR"); ok && value != "" {
		return true
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return true
	}
	return !isTerminal(os.Stdout)
}
