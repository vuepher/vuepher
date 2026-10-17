package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/internal/generator"
	"github.com/vuepher/vuepher/support"
)

// project is the context a command operates in: where the root is, what the
// module is called, and how to render stubs into it.
type project struct {
	Root       string
	Config     *projectConfig
	ModulePath string
	GoVersion  string
}

// openProject locates the project root and loads its configuration. Commands that
// need a project call this; `vuepher new`, `doctor` and `stub:publish --all` work
// without one.
func openProject() (*project, error) {
	root, err := support.ProjectRoot("")
	if err != nil {
		return nil, err
	}

	cfg, err := loadProjectConfig(root)
	if err != nil {
		return nil, err
	}

	// The framework's own minimum, used when a project has no go.mod yet.
	p := &project{Root: root, Config: cfg, GoVersion: vuepher.MinimumGoVersion}
	if module, version, err := readGoMod(filepath.Join(root, "go.mod")); err == nil {
		p.ModulePath = module
		if version != "" {
			p.GoVersion = version
		}
	}

	return p, nil
}

// path resolves a project-relative path to an absolute one.
func (p *project) path(relative string) string {
	if filepath.IsAbs(relative) {
		return relative
	}
	return filepath.Join(p.Root, relative)
}

// hasModule reports whether a go.mod exists with a module path. Generators that
// emit imports need the module path; running them outside a Go module produces a
// clear error instead of an import that cannot resolve.
func (p *project) hasModule() bool { return p.ModulePath != "" }

// requireModule returns an actionable error when the module path is unknown.
func (p *project) requireModule(command string) error {
	if p.hasModule() {
		return nil
	}
	return fmt.Errorf(
		"vuepher: %s needs a Go module, but no module path could be read from %s.\n"+
			"  Run `go mod init <module-path>` in the project root, then try again.",
		command, filepath.Join(p.Root, "go.mod"),
	)
}

// hasModel reports whether app/models/<Model>.go exists. Generators use it to
// pick between templates: a controller that references a model is nicer to read,
// but it only compiles once that model exists.
func (p *project) hasModel(model string) bool {
	if model == "" {
		return false
	}
	return support.Exists(filepath.Join(p.Root, "app", "models", model+".go"))
}

// projectStubs returns a filesystem over the project's stubs directory, or nil
// when the project has none. When it is set, stubs in it override the embedded
// ones (PRD §6.4).
func (p *project) projectStubs() fs.FS {
	dir := p.path(p.Config.Paths.Stubs)
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	return os.DirFS(dir)
}

// generator builds a stub renderer for this project. The project's stub
// directory, when present, takes precedence over the embedded templates.
func (p *project) generator(options ...generator.Option) *generator.Generator {
	all := []generator.Option{generator.WithStdout(os.Stdout)}
	if stubs := p.projectStubs(); stubs != nil {
		all = append(all, generator.WithProjectStubs(stubs))
	}
	all = append(all, options...)
	return generator.New(support.StubFS, p.Root, all...)
}

// baseData is the template payload every stub can rely on.
func (p *project) baseData() generator.Data {
	return generator.Data{
		"ProjectName":      filepath.Base(p.Root),
		"AppName":          support.Title(filepath.Base(p.Root)),
		"PackageJSONName":  support.Kebab(filepath.Base(p.Root)),
		"Module":           p.ModulePath,
		"GoVersion":        p.GoVersion,
		"FrameworkModule":  support.FrameworkModule,
		"FrameworkVersion": vuepher.Version,
		"Year":             time.Now().Year(),
		"Replace":          "",
	}
}

// namesData merges normalised names into a template payload, so a stub can use
// both project-wide values and the generator's identifiers.
func namesData(base generator.Data, names generator.Names) generator.Data {
	data := generator.Data{}
	for key, value := range base {
		data[key] = value
	}
	data["Name"] = names.Name
	data["Model"] = names.Model
	data["Camel"] = names.Camel
	data["Snake"] = names.Snake
	data["Table"] = names.Table
	data["Plural"] = names.Plural
	data["Resource"] = names.Resource
	data["Component"] = names.Component
	data["Package"] = names.Package
	data["Title"] = names.Title
	data["QueryType"] = names.QueryType
	data["QueryResultType"] = names.QueryResultType
	data["Migration"] = names.Migration
	return data
}

// readGoMod extracts the module path and the go directive from a go.mod file.
func readGoMod(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", nil
		}
		return "", "", err
	}
	defer file.Close()

	var module, version string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if module == "" && strings.HasPrefix(line, "module ") {
			module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
			module = strings.Trim(module, `"`)
			continue
		}
		if version == "" && strings.HasPrefix(line, "go ") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			// A patch version in the go directive is legal but noisy; the major
			// and minor pair is what a generated go.mod wants.
			parts := strings.Split(version, ".")
			if len(parts) > 2 {
				version = parts[0] + "." + parts[1]
			}
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return module, version, err
	}
	return module, version, nil
}
