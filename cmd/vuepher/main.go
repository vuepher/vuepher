// Command vuepher is the Vuepher command line tool: project scaffolding,
// generators, database management and the development server.
//
// Install it with:
//
//	go install github.com/vuepher/vuepher/cmd/vuepher@latest
//
// A short alias is available as a second binary:
//
//	go install github.com/vuepher/vuepher/cmd/vp@latest
//
// Both entry points build the same command tree (see internal/cli), so `vp` and
// `vuepher` can never drift apart.
package main

import (
	"os"

	"github.com/vuepher/vuepher/internal/cli"
)

func main() {
	// Every error the CLI returns is already written for a human to act on, so it
	// is printed as-is rather than wrapped in a stack trace. A non-zero exit code
	// is what makes the command usable from a script or CI.
	if err := cli.Execute(os.Args[1:]); err != nil {
		cli.PrintError(err)
		os.Exit(1)
	}
}
