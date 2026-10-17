package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// Routes are reachable as both `vuepher route:list` and `vuepher route list`.
// Laravel habits make the first the name people type, while grouped commands are
// the shape the rest of this CLI uses. Accepting both costs nothing and avoids a
// pointless "unknown command" for the most-looked-up command in the tool.
func newRouteGroupCommand() *cobra.Command {
	group := &cobra.Command{
		Use:   "route",
		Short: "Inspect the application's routes",
	}
	group.AddCommand(newRouteListCommand())
	return group
}

func newRouteListAliasCommand() *cobra.Command {
	cmd := newRouteListCommand()
	cmd.Use = "route:list"
	return cmd
}

// newRouteListCommand is the shared implementation.
func newRouteListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every registered route",
		Long: strings.TrimSpace(`
Prints a table of method, path, name and middleware count for every route the
application registers.

Routes are registered by calling functions in routes/, so the list is produced by
the application itself rather than by reading a file. The command therefore runs
inside the application binary rather than in this process.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			return project.runInternal("route:list")
		},
	}
}
