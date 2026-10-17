package cli

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher/support"
)

func newStubCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stub",
		Short: "Manage the generator stubs",
	}

	cmd.AddCommand(newStubPublishCommand())
	return cmd
}

// newStubPublishAliasCommand registers the publish command at the top level as
// `stub:publish`, the spelling the PRD documents.
func newStubPublishAliasCommand() *cobra.Command {
	cmd := newStubPublishCommand()
	cmd.Use = "stub:publish"
	return cmd
}

func newStubPublishCommand() *cobra.Command {
	var (
		force bool
		only  string
	)

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Copy the generator stubs into the project so they can be customised",
		Long: strings.TrimSpace(`
Copies every stub the generators use into stubs/ at the project root.

Once a stub is there it takes precedence over the embedded copy, so a team can
standardise the code its generators produce — a house controller style, a
different model layout — without forking the framework.

Nothing is overwritten unless --force is passed, so local edits survive re-running
the command after a framework upgrade.

Use --only to publish a single stub:

  vuepher stub:publish --only controller/resource.go.stub`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStubPublish(force, only)
		},
	}

	flags := cmd.Flags()
	flags.BoolVar(&force, "force", false, "overwrite stubs that already exist")
	flags.StringVar(&only, "only", "", "publish a single stub path, e.g. controller/resource.go.stub")
	return cmd
}

// runStubPublish copies the embedded stubs into the project.
func runStubPublish(force bool, only string) error {
	project, err := openProject()
	if err != nil {
		return err
	}

	targetDir := project.path(project.Config.Paths.Stubs)
	if targetDir == "" {
		targetDir = project.path(support.ProjectStubDir)
	}

	if only != "" {
		only = strings.TrimPrefix(filepath.ToSlash(only), "/")
		if _, err := fs.Stat(support.StubFS, only); err != nil {
			return fmt.Errorf(
				"vuepher: no stub named %q.\n"+
					"  Run `vuepher stub:publish` without --only to see them all.",
				only,
			)
		}
		return publishOne(project, targetDir, only, force)
	}

	published := 0
	var skipped []string

	err = fs.WalkDir(support.StubFS, ".", func(stubPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(stubPath, support.StubSuffix) {
			return nil
		}
		// stubs.go is the embedding package's own source, not a template.
		if path.Base(stubPath) == "stubs.go" {
			return nil
		}

		target := filepath.Join(targetDir, filepath.FromSlash(stubPath))
		if !force && support.Exists(target) {
			skipped = append(skipped, support.RelativeTo(project.Root, target))
			return nil
		}

		content, err := fs.ReadFile(support.StubFS, stubPath)
		if err != nil {
			return err
		}
		if err := support.WriteFile(target, content, true); err != nil {
			return err
		}
		published++
		return nil
	})
	if err != nil {
		return err
	}

	if published == 0 && len(skipped) == 0 {
		return fmt.Errorf("vuepher: the embedded stub set is empty; this is a packaging bug")
	}

	printf("  %s %d stub(s) into %s\n",
		styleSuccess("published"), published,
		styleInfo(support.RelativeTo(project.Root, targetDir)))
	for _, path := range skipped {
		printSkipped(path, "(already exists; --force to overwrite)")
	}
	if published > 0 {
		println()
		printf("  Edit them under %s; the generators prefer them over the built-in copies.\n",
			styleInfo(support.RelativeTo(project.Root, targetDir)))
	}
	return nil
}

// publishOne copies a single stub.
func publishOne(project *project, targetDir, stubPath string, force bool) error {
	content, err := fs.ReadFile(support.StubFS, stubPath)
	if err != nil {
		return err
	}

	target := filepath.Join(targetDir, filepath.FromSlash(stubPath))
	if !force && support.Exists(target) {
		printSkipped(support.RelativeTo(project.Root, target), "(already exists; --force to overwrite)")
		return nil
	}
	if err := support.WriteFile(target, content, true); err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, target))
	return nil
}

// stubList returns every available stub path, used by the help output and tests.
func stubList() ([]string, error) {
	var stubs []string
	err := fs.WalkDir(support.StubFS, ".", func(stubPath string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if strings.HasSuffix(stubPath, support.StubSuffix) {
			stubs = append(stubs, stubPath)
		}
		return nil
	})
	return stubs, err
}
