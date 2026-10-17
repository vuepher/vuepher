package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher"
)

// Execute runs the CLI with the given arguments (normally os.Args[1:]).
func Execute(args []string) error {
	root := NewRootCommand()
	root.SetArgs(args)
	return root.Execute()
}

// PrintError writes an error the way the CLI reports failures: to stderr, with a
// label, and without a stack trace, because every error this tool returns is
// already written for a human to act on.
func PrintError(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "%s %s\n", styleError("error:"), err.Error())
}

// NewRootCommand builds the command tree. Exported so tests can exercise
// individual commands without going through os.Args.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "vuepher",
		Short: "Full-stack MVC framework for Go and Vue 3",
		Long: strings.TrimSpace(`
Vuepher builds full-stack web applications in Go with Vue 3 as the view layer.

Routing, validation, authentication and data access all run in Go. Controllers
render Vue components through the view bridge, so there is no REST API to build
or keep in sync, and a production build is a single binary with the frontend
embedded.

Getting started:

  vuepher new blog        create a project
  cd blog
  vuepher dev             run Go and Vite together with hot reload

Run "vuepher <command> --help" for a command's options.`),
		Version:       vuepher.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(
		newNewCommand(),
		newDevCommand(),
		newBuildCommand(),
		newMakeCommand(),
		newMigrateCommand(),
		newSeedCommand(),
		newRouteGroupCommand(),
		newTypesCommand(),
		newStubCommand(),
		newDoctorCommand(),
		newVersionCommand(),
	)

	// Colon-named top-level spellings. `vuepher make:controller` cannot be a
	// subcommand of `make`, so each generator and database command is registered
	// twice: grouped for discoverability, colon-named because that is what the
	// documentation and every Laravel-shaped habit uses.
	root.AddCommand(newMakeAliasCommands()...)
	root.AddCommand(newMigrateAliasCommands()...)
	root.AddCommand(newRouteListAliasCommand(), newStubPublishAliasCommand(), newTypesGenerateAliasCommand())

	return root
}

// newVersionCommand reports the framework, Go and platform versions, which is the
// first thing to ask for in a bug report.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Vuepher, Go and platform versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printBullet("vuepher", vuepher.Version)
			printBullet("protocol", vuepher.ProtocolVersion)
			printBullet("go", runtime.Version())
			printBullet("platform", runtime.GOOS+"/"+runtime.GOARCH)
			return nil
		},
	}
}
