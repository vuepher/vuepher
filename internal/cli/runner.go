package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/vuepher/vuepher/console"
)

// runInternal executes an internal command inside the application (see the
// console package for why these cannot run in the CLI process itself).
//
// A built binary is preferred when one exists, because it is already compiled and
// because that is exactly what production runs. Falling back to `go run .` keeps
// the command usable during development, before the first build.
func (p *project) runInternal(name string, args ...string) error {
	argv := append([]string{console.InternalFlag, name}, args...)

	command, commandArgs, err := p.internalInvocation(argv)
	if err != nil {
		return err
	}

	cmd := exec.Command(command, commandArgs...)
	cmd.Dir = p.Root
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The application already printed a message; adding another would
			// bury it. Propagate the non-zero status so scripts see it.
			return fmt.Errorf("vuepher: %s failed with status %d", name, exitErr.ExitCode())
		}
		return fmt.Errorf("vuepher: could not run %s: %w", name, err)
	}
	return nil
}

// internalInvocation decides whether to run a built binary or go run.
func (p *project) internalInvocation(argv []string) (string, []string, error) {
	if binary := p.builtBinary(); binary != "" {
		return binary, argv, nil
	}

	if _, err := exec.LookPath("go"); err != nil {
		return "", nil, fmt.Errorf(
			"vuepher: neither a built binary nor the Go toolchain was found.\n"+
				"  Install Go, or run `vuepher build` first so %s exists.",
			p.Config.Build.Out,
		)
	}
	return "go", append([]string{"run", "."}, argv...), nil
}

// builtBinary returns the path to an existing built binary, or "".
func (p *project) builtBinary() string {
	if p.Config.Build.Out == "" {
		return ""
	}
	candidates := []string{p.Config.Build.Out}
	// A cross-compiled build appends the platform, and a local build may carry an
	// .exe suffix.
	candidates = append(candidates,
		p.Config.Build.Out+"-"+platformSuffix(),
		p.Config.Build.Out+".exe",
	)
	for _, candidate := range candidates {
		path := p.path(candidate)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	// The conventional Go output name is also worth checking.
	fallback := p.path(filepath.Base(p.Root))
	if info, err := os.Stat(fallback); err == nil && !info.IsDir() {
		return fallback
	}
	return ""
}

// platformSuffix renders the os-arch suffix `vuepher build` appends to a
// cross-compiled output name.
func platformSuffix() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// commandExists reports whether an executable is on PATH, for doctor.
func commandExists(name string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}
	return path, true
}
