package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher/internal/typegen"
	"github.com/vuepher/vuepher/support"
)

func newTypesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "types",
		Short: "Generate TypeScript declarations from Go",
	}

	cmd.AddCommand(newTypesGenerateCommand())
	return cmd
}

// newTypesGenerateAliasCommand registers the generator at the top level as
// `types:generate`, the spelling the PRD documents and the one this package's own
// "out of date" message tells the user to run. Without the alias that advice
// pointed at a command the CLI did not have.
func newTypesGenerateAliasCommand() *cobra.Command {
	cmd := newTypesGenerateCommand()
	cmd.Use = "types:generate"
	return cmd
}

func newTypesGenerateCommand() *cobra.Command {
	var (
		check bool
		out   string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate TypeScript declarations from Go structs",
		Long: strings.TrimSpace(`
Reads the Go structs under app/models, app/requests and app/controllers and writes
TypeScript declarations to resources/js/types/generated.

The mapping respects the ` + "`json`" + ` tag, so the property names in the
generated interfaces are the names the Vue component actually receives. Embedded
vuepher.Model expands to id, created_at and updated_at.

Two files are produced per source package, plus:

  pages.d.ts   maps each Vue component to the props type its controller sends,
               which is what makes defineProps<PageProps['Posts/Index']>() typed
               without hand-writing an interface.

  index.d.ts   a barrel that re-exports everything.

Constructs the generator cannot express become ` + "`unknown`" + ` with a warning,
rather than a plausible-looking type that is wrong.

With --check nothing is written: the command reports whether the generated files
are already up to date and exits non-zero when they are not, so CI can catch a
struct that changed without the declarations being regenerated.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			project, err := openProject()
			if err != nil {
				return err
			}

			options := typegen.Options{
				Root:    project.Root,
				Sources: project.Config.Types.Source,
				Out:     project.Config.Types.Out,
				Check:   check,
			}
			if out != "" {
				options.Out = out
			}

			printStep("reading Go structs from %s",
				styleInfo(strings.Join(project.Config.Types.Source, ", ")))

			result, err := typegen.Generate(options)
			if err != nil {
				return err
			}

			for _, warning := range result.Warnings {
				printWarning("%s", warning)
			}

			if check {
				return reportTypesCheck(project, result)
			}

			if len(result.Files) == 0 {
				println("  no structs found; nothing to generate")
				return nil
			}
			for _, path := range result.Files {
				printCreated(path)
			}
			printf("\n  %s converted, %d file(s) written\n",
				styleInfo(itoa(len(result.Types))+" types"), len(result.Files))
			if len(result.Pages) > 0 {
				printf("  %s page components mapped to props types\n", styleInfo(itoa(len(result.Pages))))
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&check, "check", false, "do not write; fail when the output is out of date")
	flags.StringVar(&out, "out", "", "override the output directory")
	return cmd
}

// reportTypesCheck implements the CI mode (TG-08).
func reportTypesCheck(project *project, result *typegen.Result) error {
	if len(result.Stale) == 0 {
		println(styleSuccess("  generated types are up to date"))
		return nil
	}

	println(styleError("  generated types are out of date:"))
	for _, path := range result.Stale {
		printf("    %s\n", styleInfo(path))
	}
	println()
	printf("  Run %s and commit the result.\n", styleInfo("vuepher types:generate"))
	return errTypesOutOfDate
}

// errTypesOutOfDate is returned by `types:generate --check` so CI fails.
var errTypesOutOfDate = &exitError{
	message: "generated types are out of date",
	code:    1,
}

// exitError carries an exit code for failures that are not really errors, such as
// a check finding a difference.
type exitError struct {
	message string
	code    int
}

func (e *exitError) Error() string { return e.message }

// ExitCode implements the interface main uses to pick a status code.
func (e *exitError) ExitCode() int { return e.code }

// relativeToProject shortens a path for output.
func relativeToProject(project *project, path string) string {
	return support.RelativeTo(project.Root, path)
}
