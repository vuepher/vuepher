// Package console implements the database commands that have to run inside the
// application binary. It is the counterpart to the `vuepher` CLI.
//
// The split exists for a reason. `vuepher` is installed standalone with
// `go install`, so it cannot import the application's packages: it has no way to
// know which migrations the application registers. Commands that need that
// knowledge therefore run in the application itself, and the CLI invokes them:
//
//	vuepher migrate
//	# becomes
//	go run . --vuepher-internal migrate
//
// The application's main function dispatches on InternalFlag, which is why
// `vuepher new` writes a few lines into main.go. Running a built binary directly
// — ./myapp --vuepher-internal migrate — works identically and is what a
// container entrypoint should use.
package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/orm"
)

// InternalFlag is the first argument that tells an application binary to run an
// internal command instead of starting the HTTP server.
const InternalFlag = "--vuepher-internal"

// Command is one internal command.
type Command struct {
	Name        string
	Description string
	Run         func(*Context) error
}

// Context carries everything an internal command needs.
type Context struct {
	// DB is the application's database handle, used by db:seed.
	DB *orm.DB

	// Routes is the application's registered route list, used by route:list.
	// It has to come from the application because routes are registered in Go
	// code, not declared in a file the CLI could read.
	Routes []vuepher.RouteInfo

	// Migrator is the application's migration registry.
	Migrator *orm.Migrator

	// Seeders is the application's seeder registry.
	Seeders *orm.SeederRegistry

	// Args are the arguments after the command name.
	Args []string

	// Stdout receives user-facing output.
	Stdout io.Writer

	// Stdin is read for confirmations.
	Stdin io.Reader

	// Confirm answers whether the user agreed, defaulting to asking on Stdin.
	Confirm func(prompt string) bool
}

// Commands lists the internal commands.
func Commands() []Command {
	return []Command{
		{Name: "migrate", Description: "Run pending migrations", Run: runMigrate},
		{Name: "migrate:rollback", Description: "Undo the last migration batch", Run: runRollback},
		{Name: "migrate:fresh", Description: "Roll everything back and migrate again", Run: runFresh},
		{Name: "migrate:status", Description: "Show each migration and whether it has run", Run: runStatus},
		{Name: "db:seed", Description: "Run database seeders", Run: runSeed},
		{Name: "route:list", Description: "List every registered route", Run: runRouteList},
	}
}

// IsInternal reports whether an argument list is an internal command.
func IsInternal(args []string) bool {
	return len(args) > 0 && args[0] == InternalFlag
}

// Run executes an internal command. args is the application's os.Args[1:].
//
// It returns handled=false when the arguments are a normal server start, so main
// can fall through to the HTTP server.
func Run(args []string, context Context) (bool, error) {
	if !IsInternal(args) {
		return false, nil
	}

	remaining := args[1:]
	if len(remaining) == 0 {
		writeList(context.Stdout)
		return true, nil
	}

	// `--vuepher-internal help` and `--help` both print the list.
	if remaining[0] == "help" || remaining[0] == "--help" || remaining[0] == "-h" {
		writeList(context.Stdout)
		return true, nil
	}

	if context.Stdout == nil {
		context.Stdout = os.Stdout
	}
	if context.Stdin == nil {
		context.Stdin = os.Stdin
	}
	if context.Confirm == nil {
		context.Confirm = newConfirmer(context.Stdin, context.Stdout)
	}

	name := remaining[0]
	context.Args = remaining[1:]

	for _, command := range Commands() {
		if command.Name != name {
			continue
		}
		return true, command.Run(&context)
	}

	return true, fmt.Errorf(
		"unknown internal command %q; available: %s",
		name, strings.Join(commandNames(), ", "),
	)
}

func commandNames() []string {
	names := make([]string, 0, len(Commands()))
	for _, command := range Commands() {
		names = append(names, command.Name)
	}
	return names
}

func writeList(stdout io.Writer) {
	fmt.Fprintln(stdout, "Internal commands (run through the application binary):")
	for _, command := range Commands() {
		fmt.Fprintf(stdout, "  %-18s %s\n", command.Name, command.Description)
	}
}

// ---------------------------------------------------------------------------
// migrate
// ---------------------------------------------------------------------------

func runMigrate(ctx *Context) error {
	if err := requireMigrator(ctx); err != nil {
		return err
	}

	report, err := ctx.Migrator.Run(context.Background())
	if err != nil {
		return err
	}
	if len(report.Applied) == 0 {
		fmt.Fprintln(ctx.Stdout, "Nothing to migrate.")
		return nil
	}
	for _, name := range report.Applied {
		fmt.Fprintf(ctx.Stdout, "migrated  %s\n", name)
	}
	fmt.Fprintf(ctx.Stdout, "\n%d migration(s) in %s (batch %d)\n",
		len(report.Applied), report.Duration.Round(time.Millisecond), report.Batch)
	return nil
}

func runRollback(ctx *Context) error {
	if err := requireMigrator(ctx); err != nil {
		return err
	}

	steps := 1
	if len(ctx.Args) > 0 {
		parsed, err := strconv.Atoi(ctx.Args[0])
		if err != nil {
			return fmt.Errorf("rollback expects a number of batches, got %q", ctx.Args[0])
		}
		steps = parsed
	}

	report, err := ctx.Migrator.Rollback(context.Background(), steps)
	if err != nil {
		return err
	}
	if len(report.RolledBack) == 0 {
		fmt.Fprintln(ctx.Stdout, "Nothing to roll back.")
		return nil
	}
	for _, name := range report.RolledBack {
		fmt.Fprintf(ctx.Stdout, "rolled back  %s\n", name)
	}
	fmt.Fprintf(ctx.Stdout, "\n%d migration(s) reverted in %s\n",
		len(report.RolledBack), report.Duration.Round(time.Millisecond))
	return nil
}

func runFresh(ctx *Context) error {
	if err := requireMigrator(ctx); err != nil {
		return err
	}

	// Dropping every table is the one thing in this tool that cannot be undone
	// from inside it, so it asks.
	if !ctx.Confirm("migrate:fresh rolls every table back and re-runs all migrations. This deletes all data. Continue?") {
		fmt.Fprintln(ctx.Stdout, "Cancelled.")
		return nil
	}

	report, err := ctx.Migrator.Fresh(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "rolled back %d migration(s), applied %d migration(s) in %s\n",
		len(report.RolledBack), len(report.Applied), report.Duration.Round(time.Millisecond))
	return nil
}

func runStatus(ctx *Context) error {
	if err := requireMigrator(ctx); err != nil {
		return err
	}

	statuses, err := ctx.Migrator.Status(context.Background())
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Fprintln(ctx.Stdout, "No migrations are registered.")
		fmt.Fprintln(ctx.Stdout, "Create one with: vuepher make:migration create_posts_table")
		return nil
	}

	fmt.Fprintf(ctx.Stdout, "%-52s %-10s %s\n", "MIGRATION", "STATUS", "BATCH")
	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = "ran"
		}
		batch := "-"
		if status.Batch > 0 {
			batch = strconv.Itoa(status.Batch)
		}
		fmt.Fprintf(ctx.Stdout, "%-52s %-10s %s\n", status.Name, state, batch)
	}
	return nil
}

// ---------------------------------------------------------------------------
// db:seed
// ---------------------------------------------------------------------------

func runSeed(ctx *Context) error {
	if ctx.Seeders == nil {
		return errors.New(
			"no seeders are registered. Register them in database/seeders/registry.go",
		)
	}
	if ctx.DB == nil {
		return errors.New("no database connection is registered")
	}
	if err := ctx.Seeders.Run(context.Background(), ctx.DB, ctx.Args...); err != nil {
		return err
	}
	fmt.Fprintln(ctx.Stdout, "Seeding complete.")
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireMigrator(ctx *Context) error {
	if ctx.Migrator == nil {
		return errors.New(
			"no migrator is registered. bootstrap should call " +
				"app.Container.Instance(\"migrator\", migrations.Registry(db))",
		)
	}
	return nil
}

// newConfirmer reads a yes/no answer from the terminal.
func newConfirmer(stdin io.Reader, stdout io.Writer) func(string) bool {
	return func(prompt string) bool {
		fmt.Fprintf(stdout, "%s [y/N] ", prompt)

		var answer string
		if _, err := fmt.Fscanln(stdin, &answer); err != nil {
			// A closed or non-interactive stdin means "no": never assume consent
			// for a destructive operation.
			fmt.Fprintln(stdout)
			return false
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
}
