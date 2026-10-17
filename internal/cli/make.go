package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher/internal/generator"
	"github.com/vuepher/vuepher/support"
)

// Package names for generated files. A file's directory determines its package,
// and the generator is told the name explicitly rather than deriving it from the
// type, because `EnsureAdmin` would otherwise produce `ensureadmin`.
const (
	pkgControllers = "controllers"
	pkgModels      = "models"
	pkgMiddleware  = "middleware"
	pkgRequests    = "requests"
	pkgMigrations  = "migrations"
	pkgSeeders     = "seeders"
	pkgFactories   = "factories"
)

// makeBuilders lists the generator constructors, in the order they appear in
// help output. Keeping them in one slice is what lets the same set be registered
// twice: once under the `make` group and once at the top level as
// `make:controller` and friends.
func makeBuilders() []func() *cobra.Command {
	return []func() *cobra.Command{
		newMakeControllerCommand,
		newMakeModelCommand,
		newMakeMiddlewareCommand,
		newMakeViewCommand,
		newMakeRequestCommand,
		newMakeMigrationCommand,
		newMakeSeederCommand,
		newMakeFactoryCommand,
	}
}

// newMakeAliasCommands registers each generator at the top level under its
// colon name.
//
// `vuepher make:controller` is the spelling the PRD specifies and the one anyone
// arriving from Laravel will type, so it has to work at the top level: a
// subcommand of `make` cannot be reached as `make:controller`.
func newMakeAliasCommands() []*cobra.Command {
	commands := make([]*cobra.Command, 0, len(makeBuilders()))
	for _, build := range makeBuilders() {
		cmd := build()
		cmd.Use = "make:" + cmd.Name()
		commands = append(commands, cmd)
	}
	return commands
}

func newMakeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "make",
		Short: "Generate application code from stubs",
		Long: strings.TrimSpace(`
Generators for the boilerplate that every application repeats.

Existing files are never overwritten: a generator tells you the file is already
there and stops, so running the wrong command cannot lose work. Pass --force to
overwrite deliberately.

Generated Go is formatted with gofmt, and a template that produces code that does
not compile is reported as a bug in the template rather than as valid output.`),
	}

	for _, build := range makeBuilders() {
		cmd.AddCommand(build())
	}
	return cmd
}

// ---------------------------------------------------------------------------
// make:controller
// ---------------------------------------------------------------------------

func newMakeControllerCommand() *cobra.Command {
	var (
		resource bool
		force    bool
	)

	cmd := &cobra.Command{
		Use:   "controller <Name>",
		Short: "Create a controller",
		Long: strings.TrimSpace(`
Creates app/controllers/<Name>.go.

The name is normalised: ` + "`vuepher make:controller Post`" + ` and
` + "`vuepher make:controller PostController`" + ` produce the same file.

With --resource the controller gets all seven RESTful actions (index, create,
store, show, edit, update, destroy), wired to a model whose name is derived from
the controller.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeController(args[0], resource, force)
		},
	}

	cmd.Flags().BoolVarP(&resource, "resource", "r", false, "include the seven CRUD actions")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

func runMakeController(raw string, resource, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:controller"); err != nil {
		return err
	}

	name := support.ControllerName(raw)
	names := generator.NamesFor(name)

	stub := "controller/controller.go.stub"
	if resource {
		// Two resource templates, chosen by whether the paired model exists.
		//
		// The model-aware one reads better and is what you want once the model is
		// there. Without the model it would not compile, because Go checks a
		// reference to a missing type at build time; the placeholder template
		// compiles standalone and shows the model calls as TODOs.
		stub = "controller/resource.go.stub"
		if !project.hasModel(names.Model) {
			stub = "controller/resource_placeholder.go.stub"
		}
		names.Component = names.Plural + "/Index"
	}

	data := namesData(project.baseData(), names)
	data["Package"] = pkgControllers

	out := filepath.Join("app", "controllers", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write(stub, out, data)
	if err != nil {
		return err
	}

	printCreated(support.RelativeTo(project.Root, path))
	printControllerHint(project, names, resource)
	return nil
}

// printControllerHint shows how to register the controller, which is the step
// people otherwise have to look up.
func printControllerHint(project *project, names generator.Names, resource bool) {
	println()
	if resource {
		printf("  Register it in %s:\n", styleInfo("routes/web.go"))
		printf("    %s\n", styleInfo(fmt.Sprintf(`app.Resource("%s", &controllers.%s{})`, names.Table, names.Name)))
		if !project.hasModel(names.Model) {
			printf("\n  The bodies are TODO stubs: there is no %s model yet, and Go cannot compile a\n", styleInfo(names.Model))
			printf("  reference to a type that does not exist. Create it with:\n")
			printf("    %s\n", styleInfo(fmt.Sprintf("vuepher make:model %s -m", names.Model)))
		}
	} else {
		printf("  Register it in %s:\n", styleInfo("routes/web.go"))
		printf("    %s\n", styleInfo(fmt.Sprintf(`app.Get("/%s", (&controllers.%s{}).Index)`, names.Resource, names.Name)))
	}
	println()
}

// ---------------------------------------------------------------------------
// make:model
// ---------------------------------------------------------------------------

func newMakeModelCommand() *cobra.Command {
	var (
		withMigration  bool
		withController bool
		all            bool
		force          bool
	)

	cmd := &cobra.Command{
		Use:   "model <Name>",
		Short: "Create a model",
		Long: strings.TrimSpace(`
Creates app/models/<Name>.go with the base model embedded, a TableName method and
a Query method that starts a fluent query:

  posts, err := models.Post{}.Query().Where("published = ?", true).Latest().Get()

Flags:

  -m, --migration   also create a migration that creates the table
  -c, --controller  also create a resource controller
  -a, --all         both of the above`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all {
				withMigration, withController = true, true
			}
			return runMakeModel(args[0], withMigration, withController, force)
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&withMigration, "migration", "m", false, "also create a migration")
	flags.BoolVarP(&withController, "controller", "c", false, "also create a resource controller")
	flags.BoolVarP(&all, "all", "a", false, "create the migration and the controller")
	flags.BoolVar(&force, "force", false, "overwrite files that already exist")
	return cmd
}

func runMakeModel(raw string, withMigration, withController, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:model"); err != nil {
		return err
	}

	names := generator.NamesFor(raw)
	data := namesData(project.baseData(), names)
	data["Package"] = pkgModels

	out := filepath.Join("app", "models", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write("model/model.go.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))

	if withMigration {
		if _, err := createMigration(project, "create_"+names.Table+"_table", names.Model, force); err != nil {
			return err
		}
	}

	if withController {
		controllerNames := generator.NamesFor(support.ControllerName(names.Name))
		controllerNames.Component = controllerNames.Plural + "/Index"
		controllerData := namesData(project.baseData(), controllerNames)
		controllerData["Package"] = pkgControllers

		// The model is being created by this command, so the model-aware template
		// is the right one: it will compile as soon as the model file above lands.
		controllerOut := filepath.Join("app", "controllers", controllerNames.Name+".go")
		controllerPath, err := project.generator(generator.WithForce(force)).
			Write("controller/resource.go.stub", controllerOut, controllerData)
		if err != nil {
			return err
		}
		printCreated(support.RelativeTo(project.Root, controllerPath))
	}

	println()
	println(styleHeading("  Next:"))
	printf("    add your columns to %s\n", styleInfo(filepath.ToSlash(out)))
	if withMigration {
		printf("    %s\n", styleInfo("vuepher migrate"))
	}
	println()
	return nil
}

// ---------------------------------------------------------------------------
// make:middleware
// ---------------------------------------------------------------------------

func newMakeMiddlewareCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "middleware <Name>",
		Short: "Create a middleware",
		Long: strings.TrimSpace(`
Creates app/middleware/<Name>.go.

Middleware receives the typed context and a continuation:

  return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
      if !allowed(ctx) {
          return vuepher.Forbidden("You cannot do that.")
      }
      return next(ctx)
  }`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeMiddleware(args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

func runMakeMiddleware(raw string, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:middleware"); err != nil {
		return err
	}

	names := generator.NamesFor(raw)
	data := namesData(project.baseData(), names)
	data["Package"] = pkgMiddleware

	out := filepath.Join("app", "middleware", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write("middleware/middleware.go.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))

	println()
	printf("  Register it with %s\n", styleInfo(fmt.Sprintf("middleware.%s()", names.Name)))
	return nil
}

// ---------------------------------------------------------------------------
// make:view
// ---------------------------------------------------------------------------

func newMakeViewCommand() *cobra.Command {
	var (
		force      bool
		controller string
	)

	cmd := &cobra.Command{
		Use:   "view <Folder/Name>",
		Short: "Create a Vue page component",
		Long: strings.TrimSpace(`
Creates resources/js/Pages/<Folder/Name>.vue.

The name maps directly onto the component string a controller passes to
ctx.View, so ` + "`make:view Posts/Index`" + ` creates the component that
` + "`ctx.View(\"Posts/Index\", props)`" + ` resolves.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeView(args[0], force, controller)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	cmd.Flags().StringVar(&controller, "controller", "", "controller name, used to look up the props type")
	return cmd
}

func runMakeView(raw string, force bool, controllerName string) error {
	project, err := openProject()
	if err != nil {
		return err
	}

	names := generator.NamesForView(raw)
	if names.Component == "" {
		return fmt.Errorf("vuepher: %q is not a valid view name; try Posts/Index", raw)
	}

	data := namesData(project.baseData(), names)
	out := filepath.Join(project.Config.Paths.Pages, names.Component+".vue")
	path, err := project.generator(generator.WithForce(force)).Write("view/page.vue.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))

	// PRD §6.4: make:view links the props type when a controller for the same
	// resource already renders this component. The link is reported rather than
	// silently applied, because the generated component is then edited by hand.
	if linked := findPropsType(project, names.Component, controllerName); linked != "" {
		println()
		printf("  Props type %s was found. Wire it up with:\n", styleInfo(linked))
		printf("    %s\n", styleInfo(fmt.Sprintf(
			"import type { PageProps } from '@/types/generated/pages'\ndefineProps<PageProps['%s']>()",
			names.Component,
		)))
	}
	return nil
}

// findPropsType looks for a generated declaration matching a component, so
// make:view can point at it. It is best-effort: no match is not an error.
func findPropsType(project *project, component, controllerName string) string {
	_ = component
	_ = controllerName
	// The authoritative mapping is produced by `vuepher types:generate` into
	// PageProps. Reading it here would require parsing the generated file, so the
	// hint is left generic until TG-06's incremental generation lands.
	return ""
}

// ---------------------------------------------------------------------------
// make:request
// ---------------------------------------------------------------------------

func newMakeRequestCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "request <Name>",
		Short: "Create a validated request struct",
		Long: strings.TrimSpace(`
Creates app/requests/<Name>.go.

The struct declares both the accepted input and its rules:

  type StorePost struct {
      Title string ` + "`json:\"title\" validate:\"required,min=3,max=120\"`" + `
  }

A controller then does:

  var req requests.StorePost
  if err := ctx.Validate(&req); err != nil {
      return ctx.BackWithErrors(err)
  }`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeRequest(args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

func runMakeRequest(raw string, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}

	names := generator.NamesFor(raw)
	data := namesData(project.baseData(), names)
	data["Package"] = pkgRequests

	out := filepath.Join("app", "requests", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write("request/request.go.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))
	return nil
}

// ---------------------------------------------------------------------------
// make:migration
// ---------------------------------------------------------------------------

func newMakeMigrationCommand() *cobra.Command {
	var (
		force bool
		model string
	)

	cmd := &cobra.Command{
		Use:   "migration <name>",
		Short: "Create a migration",
		Long: strings.TrimSpace(`
Creates database/migrations/<timestamp>_<name>.go.

The timestamp prefix is what orders migrations, so the file always lands in the
right place regardless of when it was written.

Remember to register the migration in database/migrations/registry.go: nothing
scans the directory, so a file that is not registered does not run.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeMigration(args[0], model, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	cmd.Flags().StringVar(&model, "model", "", "model the migration operates on (default: derived from the name)")
	return cmd
}

func runMakeMigration(raw, modelName string, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:migration"); err != nil {
		return err
	}
	_, err = createMigration(project, raw, modelName, force)
	return err
}

// createMigration is shared by make:migration and make:model -m.
func createMigration(project *project, raw, modelName string, force bool) (string, error) {
	timestamp := support.MigrationTimestamp(time.Now())
	names := generator.NamesForMigration(raw, timestamp)

	if modelName != "" {
		names = names.WithModel(modelName)
	} else if trimmed := strings.TrimPrefix(support.MigrationName(raw), "create_"); trimmed != raw {
		// "create_posts_table" names the model "post".
		tableName := strings.TrimSuffix(strings.TrimSuffix(trimmed, "_table"), "_")
		if tableName != "" {
			names = names.WithModel(support.Singular(tableName))
		}
	}

	data := namesData(project.baseData(), names)
	data["Package"] = pkgMigrations
	data["Model"] = names.Model

	out := filepath.Join(project.Config.Paths.Migrations, names.Migration+".go")
	path, err := project.generator(generator.WithForce(force)).Write("migration/migration.go.stub", out, data)
	if err != nil {
		return "", err
	}
	printCreated(support.RelativeTo(project.Root, path))

	println()
	printf("  Register it in %s:\n", styleInfo("database/migrations/registry.go"))
	printf("    %s\n", styleInfo(fmt.Sprintf(
		`migrator.Register("%s", migrations.%s{})`, names.Migration, names.Name,
	)))
	println()
	return path, nil
}

// ---------------------------------------------------------------------------
// make:seeder
// ---------------------------------------------------------------------------

func newMakeSeederCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "seeder <Name>",
		Short: "Create a database seeder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeSeeder(args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

func runMakeSeeder(raw string, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:seeder"); err != nil {
		return err
	}

	names := generator.NamesFor(raw)
	data := namesData(project.baseData(), names)
	data["Package"] = pkgSeeders

	out := filepath.Join("database", "seeders", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write("seeder/seeder.go.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))

	println()
	printf("  Register it in %s, then run %s\n",
		styleInfo("database/seeders/registry.go"), styleInfo("vuepher db:seed "+names.Name))
	return nil
}

// ---------------------------------------------------------------------------
// make:factory
// ---------------------------------------------------------------------------

func newMakeFactoryCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "factory <Name>",
		Short: "Create a model factory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMakeFactory(args[0], force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite the file if it already exists")
	return cmd
}

func runMakeFactory(raw string, force bool) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("make:factory"); err != nil {
		return err
	}

	names := generator.NamesFor(raw)
	data := namesData(project.baseData(), names)
	data["Package"] = pkgFactories

	out := filepath.Join("database", "factories", names.Name+".go")
	path, err := project.generator(generator.WithForce(force)).Write("factory/factory.go.stub", out, data)
	if err != nil {
		return err
	}
	printCreated(support.RelativeTo(project.Root, path))
	return nil
}
