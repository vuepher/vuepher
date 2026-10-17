// Command vp is the short alias for the Vuepher CLI (PRD §6.4).
//
// It is a separate main package rather than a shell alias so that
//
//	go install github.com/vuepher/vuepher/cmd/vp@latest
//
// produces a real binary on every platform, including Windows, where an alias in
// a shell profile is not how anyone works. The command tree itself is the same
// one `vuepher` builds, so the two can never drift.
package main

import (
	"os"

	"github.com/vuepher/vuepher/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:]); err != nil {
		cli.PrintError(err)
		os.Exit(1)
	}
}
