// Package generator renders the embedded stub templates into project files.
//
// It is the single place where generator behaviour lives, so that every
// `make:*` command behaves the same way: names normalised, existing files
// preserved unless --force, generated Go formatted, and a broken stub reported
// against the stub that produced it rather than against the output.
package generator

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/vuepher/vuepher/support"
)

// Delimiters for stub templates. Vue single-file components use `{{ }}` for
// interpolation, and those files are among the things generated, so the
// templates use a pair Vue does not use.
var (
	LeftDelimiter  = "[["
	RightDelimiter = "]]"
)

// Data is the payload handed to a stub. Keys are the names a template refers to
// as .Name, .Table and so on. It is a map rather than a struct so that each
// generator can add its own fields without a struct per artefact type.
type Data map[string]any

// Get reads a field, returning the zero string when it is absent. Useful in
// generator code that composes payloads.
func (d Data) Get(key string) string {
	value, ok := d[key]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// Merge returns a new payload with another layered on top.
func (d Data) Merge(other Data) Data {
	out := make(Data, len(d)+len(other))
	for key, value := range d {
		out[key] = value
	}
	for key, value := range other {
		out[key] = value
	}
	return out
}

// Generator renders stubs into a project.
type Generator struct {
	embedded     fs.FS
	projectStubs fs.FS
	root         string
	force        bool
	funcs        template.FuncMap
	clock        func() time.Time
	stdout       io.Writer
}

// Option configures a Generator.
type Option func(*Generator)

// WithForce allows overwriting existing files.
func WithForce(force bool) Option {
	return func(g *Generator) { g.force = force }
}

// WithProjectStubs installs a project-local stub directory that takes precedence
// over the embedded set, which is what `vuepher stub:publish` enables.
func WithProjectStubs(fsys fs.FS) Option {
	return func(g *Generator) { g.projectStubs = fsys }
}

// WithStdout sets where progress is reported.
func WithStdout(w io.Writer) Option {
	return func(g *Generator) { g.stdout = w }
}

// WithClock overrides the time source, so tests produce stable migration names.
func WithClock(clock func() time.Time) Option {
	return func(g *Generator) { g.clock = clock }
}

// New builds a generator rooted at a project directory.
func New(embedded fs.FS, root string, options ...Option) *Generator {
	g := &Generator{
		embedded: embedded,
		root:     root,
		funcs:    standardFuncs(),
		clock:    time.Now,
		stdout:   io.Discard,
	}
	for _, option := range options {
		option(g)
	}
	return g
}

// Root returns the project directory output is written under.
func (g *Generator) Root() string { return g.root }

// Force reports whether overwriting is enabled.
func (g *Generator) Force() bool { return g.force }

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// RenderString renders a template held in memory. Used by tests and by callers
// that compose a template rather than load one.
func (g *Generator) RenderString(name, source string, data any) (string, error) {
	tmpl, err := template.New(name).
		Delims(LeftDelimiter, RightDelimiter).
		Funcs(g.funcs).
		Option("missingkey=error").
		Parse(source)
	if err != nil {
		return "", fmt.Errorf("vuepher: stub %s could not be parsed: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		// missingkey=error means a typo in a stub is reported with the key name,
		// which is far more useful than an empty string in the output.
		return "", fmt.Errorf("vuepher: stub %s could not be rendered: %w", name, err)
	}
	return buf.String(), nil
}

// Render renders a stub and returns the result without writing it.
func (g *Generator) Render(stubPath string, data any) (string, error) {
	source, err := g.readStub(stubPath)
	if err != nil {
		return "", err
	}
	return g.RenderString(stubPath, string(source), data)
}

// Write renders a stub and writes it to outPath, relative to the project root
// unless it is absolute.
//
// A `.go` output is formatted; if the formatted result is not valid Go, the
// error names the stub, because the fix belongs in the template.
func (g *Generator) Write(stubPath, outPath string, data any) (string, error) {
	rendered, err := g.Render(stubPath, data)
	if err != nil {
		return "", err
	}

	if strings.HasSuffix(outPath, ".go") {
		formatted, formatErr := format.Source([]byte(rendered))
		if formatErr != nil {
			return "", fmt.Errorf(
				"vuepher: stub %s produced Go that does not compile: %v\n"+
					"  This is a bug in the stub, not in your input. Fix %s and try again.",
				stubPath, formatErr, stubPath,
			)
		}
		rendered = string(formatted)
	}

	target := g.resolve(outPath)
	if err := support.WriteFile(target, []byte(rendered), g.force); err != nil {
		if errors.Is(err, support.ErrFileExists) {
			return "", g.alreadyExistsError(target, outPath)
		}
		return "", err
	}
	return target, nil
}

// alreadyExistsError explains what to do rather than just refusing.
func (g *Generator) alreadyExistsError(target, shown string) error {
	return fmt.Errorf(
		"vuepher: %s already exists.\n"+
			"  Nothing was changed. Pass --force to overwrite it, or pick another name.",
		shown,
	)
}

// WriteTree renders every stub under stubDir into outDir, stripping the `.stub`
// suffix. skip receives each stub-relative path and can exclude files, which the
// CLI uses to omit optional parts of the application skeleton.
//
// It returns the paths written, relative to the project root.
func (g *Generator) WriteTree(stubDir, outDir string, data any, skip func(relative string) bool) ([]string, error) {
	var written []string

	err := fs.WalkDir(g.embeddedFor(stubDir), stubDir, func(stubPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(stubPath, support.StubSuffix) {
			// Anything without the suffix is not a template; skipping it keeps a
			// stray file in the stubs directory from breaking generation.
			return nil
		}

		relative := strings.TrimPrefix(stubPath, stubDir+"/")
		if skip != nil && skip(relative) {
			return nil
		}

		outPath := filepath.Join(outDir, strings.TrimSuffix(relative, support.StubSuffix))
		path, err := g.Write(stubPath, outPath, data)
		if err != nil {
			return err
		}
		written = append(written, path)
		return nil
	})
	if err != nil {
		return written, err
	}
	return written, nil
}

// embeddedFor returns the filesystem a stub directory should be read from,
// preferring project overrides.
func (g *Generator) embeddedFor(stubDir string) fs.FS {
	if g.projectStubs != nil {
		if _, err := fs.Stat(g.projectStubs, stubDir); err == nil {
			// A merged filesystem would be ideal, but an all-or-nothing override
			// per directory is easier to reason about and covers the real need:
			// re-point a generator at your own template.
			return g.projectStubs
		}
	}
	return g.embedded
}

// readStub loads a stub, preferring a project override of the same path.
func (g *Generator) readStub(stubPath string) ([]byte, error) {
	if g.projectStubs != nil {
		if content, err := fs.ReadFile(g.projectStubs, stubPath); err == nil {
			return content, nil
		}
	}
	content, err := fs.ReadFile(g.embedded, stubPath)
	if err != nil {
		return nil, fmt.Errorf(
			"vuepher: stub %s is missing from the embedded set. This is a packaging bug; "+
				"reinstall the CLI or run `vuepher doctor`.",
			stubPath,
		)
	}
	return content, nil
}

// resolve turns an output path into an absolute path under the project root.
func (g *Generator) resolve(outPath string) string {
	if filepath.IsAbs(outPath) {
		return outPath
	}
	if g.root == "" {
		return outPath
	}
	return filepath.Join(g.root, outPath)
}
