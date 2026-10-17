package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run pending database migrations",
		Long: strings.TrimSpace(`
Applies every migration that has not run yet, recording each one in the
` + "`migrations`" + ` table with the batch it belongs to.

Migrations live in database/migrations and must be registered in
database/migrations/registry.go: nothing scans the directory, so a file that is
not registered does not run.

Because migrations are registered in Go code, this command runs inside the
application rather than in the CLI process. It prefers a binary produced by
` + "`vuepher build`" + ` and falls back to ` + "`go run .`" + ` during development.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			return project.runInternal("migrate")
		},
	}

	cmd.AddCommand(
		newMigrateRollbackCommand(),
		newMigrateFreshCommand(),
		newMigrateStatusCommand(),
	)
	return cmd
}

// newMigrateAliasCommands registers rollback, fresh and status at the top level
// under their colon names, so `vuepher migrate:status` works alongside
// `vuepher migrate status`.
func newMigrateAliasCommands() []*cobra.Command {
	commands := []*cobra.Command{
		newMigrateRollbackCommand(),
		newMigrateFreshCommand(),
		newMigrateStatusCommand(),
	}
	for _, cmd := range commands {
		cmd.Use = "migrate:" + cmd.Name()
	}
	return commands
}

func newMigrateRollbackCommand() *cobra.Command {
	var steps int

	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Undo the last migration batch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			argv := []string{}
			if steps > 1 {
				argv = append(argv, itoa(steps))
			}
			return project.runInternal("migrate:rollback", argv...)
		},
	}

	cmd.Flags().IntVar(&steps, "step", 1, "how many batches to roll back")
	return cmd
}

func newMigrateFreshCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "fresh",
		Short: "Roll every migration back and run them again",
		Long: strings.TrimSpace(`
Drops every table and re-runs all migrations from scratch.

This deletes all data. The command asks for confirmation before doing anything,
and refuses when standard input is not a terminal — so a script cannot run it by
accident.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			return project.runInternal("migrate:fresh")
		},
	}
}

func newMigrateStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show each migration and whether it has run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			return project.runInternal("migrate:status")
		},
	}
}

// itoa avoids importing strconv for one call.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
