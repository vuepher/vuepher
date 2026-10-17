package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func newSeedCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "db:seed [seeder...]",
		Short: "Seed the database",
		Long: strings.TrimSpace(`
Runs the application's seeders.

With no arguments every registered seeder runs in registration order; naming one
or more runs only those, which is what you want while iterating on a single
seeder:

  vuepher db:seed
  vuepher db:seed PostSeeder

Seeders live in database/seeders and are registered in
database/seeders/registry.go. They should be safe to run repeatedly: seed data
that only works on an empty database is a trap for the next person.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}
			return project.runInternal("db:seed", args...)
		},
	}
}
