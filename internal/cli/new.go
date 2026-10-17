package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/internal/generator"
	"github.com/vuepher/vuepher/support"
)

// newOptions collects the flags of `vuepher new`.
type newOptions struct {
	Name         string
	ModulePath   string
	Database     string
	Link         string
	FrameworkVer string
	GoPort       int
	VitePort     int
	NoInstall    bool
	NoGit        bool
	Force        bool
}

func newNewCommand() *cobra.Command {
	options := newOptions{}

	cmd := &cobra.Command{
		Use:   "new <project>",
		Short: "Create a new Vuepher project",
		Long: strings.TrimSpace(`
Creates a project directory containing a working Vuepher application: Go module,
Vite + Vue 3 + TypeScript frontend, a migrated database, routes, and an example
CRUD screen.

After it finishes, the project runs with:

  cd <project>
  vuepher dev

Node.js is required for development and for a production build, but not to run a
built binary.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Name = args[0]
			options.Defaults()
			return runNew(options)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&options.ModulePath, "module", "", "Go module path (default: the project name)")
	flags.StringVar(&options.Database, "db", "sqlite", "database driver: sqlite, postgres or mysql")
	flags.StringVar(&options.Link, "link", "", "path to a local Vuepher checkout, added as a replace directive")
	flags.StringVar(&options.FrameworkVer, "framework-version", vuepher.Version, "framework version to require")
	flags.IntVar(&options.GoPort, "port", 8080, "port the Go server listens on in development")
	flags.IntVar(&options.VitePort, "vite-port", 5173, "port the Vite dev server listens on")
	flags.BoolVar(&options.NoInstall, "no-install", false, "skip npm install and go mod tidy")
	flags.BoolVar(&options.NoGit, "no-git", false, "skip git init")
	flags.BoolVar(&options.Force, "force", false, "create the project even if the directory is not empty")

	return cmd
}

// Defaults fills in derived options.
func (o *newOptions) Defaults() {
	o.Name = strings.TrimSpace(o.Name)
	if o.ModulePath == "" {
		// A bare name is a valid module path for a local project, and it is what
		// `go mod init` would accept without an import that fails to resolve.
		o.ModulePath = strings.ToLower(support.Kebab(o.Name))
	}
	if o.FrameworkVer == "" {
		o.FrameworkVer = vuepher.Version
	}
	if o.GoPort == 0 {
		o.GoPort = 8080
	}
	if o.VitePort == 0 {
		o.VitePort = 5173
	}
}

// runNew scaffolds a project.
func runNew(options newOptions) error {
	target, err := filepath.Abs(options.Name)
	if err != nil {
		return err
	}

	// Refuse to scribble into a directory with content in it: the failure mode of
	// getting this wrong is losing someone's work.
	if support.Exists(target) {
		if !support.IsDir(target) {
			return fmt.Errorf("vuepher: %s already exists and is not a directory", target)
		}
		if !support.IsEmptyDir(target) && !options.Force {
			return fmt.Errorf(
				"vuepher: %s is not empty.\n"+
					"  Nothing was written. Choose another name, or pass --force to write into it anyway.",
				target,
			)
		}
	}

	if !isSupportedDatabase(options.Database) {
		return fmt.Errorf(
			"vuepher: --db %q is not supported; choose sqlite, postgres or mysql",
			options.Database,
		)
	}

	appKey, err := generateAppKey()
	if err != nil {
		return err
	}

	// The framework is required as a module, and during development on Vuepher
	// itself it is also replaced with a local checkout so that edits to the
	// framework take effect without publishing.
	replace := ""
	vue3Dependency := "^" + options.FrameworkVer
	if options.Link != "" {
		linkPath, err := filepath.Abs(options.Link)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(linkPath, "go.mod")); err != nil {
			return fmt.Errorf(
				"vuepher: --link %s does not look like a Vuepher checkout (no go.mod there)",
				linkPath,
			)
		}
		replace = fmt.Sprintf("\nreplace %s => %s\n", support.FrameworkModule, filepath.ToSlash(linkPath))

		// The npm package lives next to the Go module in the framework checkout,
		// so the frontend can be linked the same way.
		packagePath := filepath.Join(linkPath, "packages", "vue3")
		if support.IsDir(packagePath) {
			if relative, err := filepath.Rel(target, packagePath); err == nil {
				vue3Dependency = "file:" + filepath.ToSlash(relative)
			}
		}
	}

	// The skeleton is rendered with an empty APP_KEY so that .env.example stays a
	// template; the real key is written to .env afterwards.
	data := generator.Data{
		"ProjectName":      filepath.Base(target),
		"AppName":          support.Title(filepath.Base(target)),
		"PackageJSONName":  support.Kebab(filepath.Base(target)),
		"Module":           options.ModulePath,
		"GoVersion":        vuepher.MinimumGoVersion,
		"FrameworkModule":  support.FrameworkModule,
		"FrameworkVersion": options.FrameworkVer,
		"Year":             2026,
		"Replace":          replace,
		"DBConnection":     options.Database,
		"GoPort":           options.GoPort,
		"VitePort":         options.VitePort,
		"AppKey":           "",
		"Vue3Dependency":   vue3Dependency,
	}

	gen := generator.New(support.StubFS, target, generator.WithForce(options.Force))

	printStep("scaffolding %s", styleInfo(support.RelativeTo(mustGetwd(), target)))
	written, err := gen.WriteTree("app", "", data, nil)
	if err != nil {
		return err
	}
	for _, path := range written {
		printCreated(support.RelativeTo(mustGetwd(), path))
	}

	// .env holds the generated key and is not committed.
	examplePath := filepath.Join(target, ".env.example")
	if err := writeEnvFile(examplePath, filepath.Join(target, ".env"), appKey); err != nil {
		return err
	}
	printCreated(support.RelativeTo(mustGetwd(), filepath.Join(target, ".env")))

	if !options.NoGit {
		if err := initGit(target); err != nil {
			// A missing git binary should not fail project creation.
			printWarning("could not initialise a git repository: %v", err)
		}
	}

	if !options.NoInstall {
		if err := installDependencies(target); err != nil {
			return err
		}
	}

	printNextSteps(target, options)
	return nil
}

// writeEnvFile copies the example environment file to .env with a generated
// application key.
func writeEnvFile(examplePath, envPath, appKey string) error {
	content, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("vuepher: could not read %s: %w", examplePath, err)
	}
	env := strings.Replace(string(content), "APP_KEY=", "APP_KEY="+appKey, 1)
	return os.WriteFile(envPath, []byte(env), 0o600)
}

// initGit creates a repository and an initial commit.
func initGit(dir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is not installed")
	}
	if output, err := runIn(dir, "git", "init", "--quiet"); err != nil {
		return fmt.Errorf("%s", output)
	}
	return nil
}

// installDependencies fetches the Go modules and the npm packages.
func installDependencies(dir string) error {
	printStep("fetching Go modules")
	if output, err := runIn(dir, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf(
			"vuepher: `go mod tidy` failed:\n%s\n"+
				"  Fix the error and run it again in %s.",
			indent(output), dir,
		)
	}

	printStep("installing frontend dependencies (this takes a moment)")
	if _, err := exec.LookPath("npm"); err != nil {
		printWarning("npm was not found; run `npm install` in %s once Node.js is available", dir)
		return nil
	}
	if output, err := runIn(dir, "npm", "install", "--no-audit", "--no-fund"); err != nil {
		return fmt.Errorf(
			"vuepher: `npm install` failed:\n%s\n"+
				"  Fix the error and run it again in %s.",
			indent(output), dir,
		)
	}
	return nil
}

// printNextSteps tells the user exactly what to run, which is the difference
// between a project that starts and one that sits unused.
func printNextSteps(target string, options newOptions) {
	println()
	println(styleSuccess("Project created."))
	println()
	println(styleHeading("  Next:"))
	if target != mustGetwd() {
		printf("    cd %s\n", styleInfo(filepath.Base(target)))
	}
	if options.NoInstall {
		printf("    %s\n", "go mod tidy && npm install")
	}
	printf("    %-34s %s\n", styleInfo("vuepher migrate"), styleMuted("# create the database tables"))
	printf("    %-34s %s\n", styleInfo("vuepher db:seed"), styleMuted("# optional: example data"))
	printf("    %-34s %s\n", styleInfo("vuepher dev"), styleMuted("# start Go and Vite"))
	println()
	printf("  Then open %s\n", styleInfo(fmt.Sprintf("http://localhost:%d", options.GoPort)))
	printf("  Demo account: %s / %s\n", styleInfo("demo@example.com"), styleInfo("password"))
	println()
}

// isSupportedDatabase reports whether a driver name is supported.
func isSupportedDatabase(name string) bool {
	switch strings.ToLower(name) {
	case "sqlite", "sqlite3", "postgres", "postgresql", "pgsql", "mysql", "mariadb":
		return true
	default:
		return false
	}
}

// generateAppKey produces a 32-byte random key, hex encoded.
func generateAppKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("vuepher: could not generate an application key: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// runIn executes a command in a directory and returns its combined output.
func runIn(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// indent prefixes every line, for embedding command output in an error message.
func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

// mustGetwd returns the working directory, or "." when it cannot be read. Used
// only for shortening paths in output, so a failure is not worth propagating.
func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
