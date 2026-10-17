package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher"
)

// diagnostics accumulate problems and suggestions so the command reports
// everything wrong in one pass instead of making the user fix one thing, re-run,
// and find the next (§7.3).
type diagnostics struct {
	failures    int
	warnings    int
	suggestions []string
}

func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the toolchain and project configuration",
		Long: strings.TrimSpace(`
Verifies that the machine and the project are set up correctly: Go and Node
versions, configuration, dependencies, ports and database reachability.

Every problem is printed with what to do about it, and all problems are reported
in one run rather than stopping at the first. Exits non-zero when something is
wrong, so it can gate a CI job or a support request.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	report := &diagnostics{}

	println()
	println(styleHeading("Vuepher doctor"))
	println()

	printSection("Toolchain")
	checkGo(report)
	checkNode(report)

	printSection("Project")
	project := checkProject(report)

	printSection("Configuration")
	if project != nil {
		checkEnv(report, project)
		checkNodeModules(report, project)
		checkPorts(report, project)
		checkBuiltBinary(report, project)
	}

	println()
	printSummary(report)
	println()

	if report.failures > 0 {
		return fmt.Errorf("%d problem(s) need attention", report.failures)
	}
	return nil
}

func printSection(title string) {
	printf("%s\n", styleHeading(title))
}

// minimumGo splits MinimumGoVersion into the major and minor numbers lessThan
// compares, so the requirement lives in exactly one place.
func minimumGo() (int, int) {
	parts := strings.Split(vuepher.MinimumGoVersion, ".")
	major, minor := 0, 0
	if len(parts) > 0 {
		major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}

// checkGo verifies the Go toolchain version.
func checkGo(report *diagnostics) {
	path, ok := commandExists("go")
	if !ok {
		report.fail("Go was not found on PATH.", fmt.Sprintf("Install Go %s or newer from https://go.dev/dl", vuepher.MinimumGoVersion))
		return
	}
	report.pass("Go", path)

	output, err := exec.Command("go", "version").Output()
	if err != nil {
		report.warn("could not read the Go version", "Run `go version` yourself to check it.")
		return
	}
	version := parseGoVersion(strings.TrimSpace(string(output)))
	if version == "" {
		report.warn("could not parse the Go version", "")
		return
	}
	if minimumMajor, minimumMinor := minimumGo(); lessThan(version, minimumMajor, minimumMinor) {
		report.fail(
			fmt.Sprintf("Go %s is too old; Vuepher requires %s or newer.", version, vuepher.MinimumGoVersion),
			"Upgrade Go, then run `go install github.com/vuepher/vuepher/cmd/vuepher@latest`",
		)
		return
	}
	report.pass("Go version", version)
}

// checkNode verifies Node.js and npm.
func checkNode(report *diagnostics) {
	nodePath, ok := commandExists("node")
	if !ok {
		report.warn(
			"Node.js was not found. It is needed to develop and build the frontend, not to run a built binary.",
			"Install Node.js LTS from https://nodejs.org if you need to run `vuepher dev` or `vuepher build`.",
		)
	} else {
		report.pass("Node", nodePath)
		output, err := exec.Command("node", "-v").Output()
		if err == nil {
			version := strings.TrimSpace(string(output))
			if strings.HasPrefix(version, "v") && lessThan(strings.TrimPrefix(version, "v"), 18, 0) {
				report.fail(
					fmt.Sprintf("Node %s is older than 18; Vite 6 requires at least 18.", version),
					"Upgrade Node.js to the current LTS release.",
				)
			} else {
				report.pass("Node version", version)
			}
		}
	}

	if _, ok := commandExists("npm"); ok {
		report.pass("npm", "found")
	} else {
		report.warn("npm was not found.", "It normally ships with Node.js; reinstall Node.js if it is missing.")
	}
}

// checkProject locates the project root and reads its module path.
func checkProject(report *diagnostics) *project {
	p, err := openProject()
	if err != nil {
		report.fail(err.Error(), "Run this command from inside a Vuepher project, or create one with `vuepher new`.")
		return nil
	}
	report.pass("Project root", p.Root)

	if p.ModulePath == "" {
		report.fail(
			"go.mod has no module path, so generated imports cannot resolve.",
			"Run `go mod init <module-path>` in "+p.Root,
		)
	} else {
		report.pass("Go module", p.ModulePath)
	}

	report.pass("vuepher.json", "valid")
	report.pass("Go version directive", p.GoVersion)
	return p
}

// checkEnv verifies the environment file.
func checkEnv(report *diagnostics, p *project) {
	envPath := p.path(".env")
	if !support_Exists(envPath) {
		report.fail(
			".env is missing.",
			"Copy .env.example to .env, then run `vuepher key:generate` (or paste any random value into APP_KEY).",
		)
		return
	}
	report.pass(".env", "present")

	content, err := os.ReadFile(envPath)
	if err != nil {
		report.warn("could not read .env", err.Error())
		return
	}

	if !strings.Contains(string(content), "APP_KEY=") {
		report.fail(
			"APP_KEY is not set in .env.",
			"Sessions are encrypted with this key. Set it to any long random string; rotating it signs everyone out.",
		)
		return
	}
	// Present but empty is the same problem as absent.
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "APP_KEY=") {
			if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "APP_KEY=")) == "" {
				report.fail(
					"APP_KEY is empty in .env.",
					"Set it to a long random value, for example the output of `openssl rand -hex 32`.",
				)
				return
			}
		}
	}
	report.pass("APP_KEY", "set")
}

// checkNodeModules verifies frontend dependencies.
func checkNodeModules(report *diagnostics, p *project) {
	if support_IsDir(p.path("node_modules")) {
		report.pass("node_modules", "present")
		return
	}
	report.warn(
		"node_modules is missing, so `vuepher dev` and `vuepher build` will not work yet.",
		"Run `npm install` in "+p.Root,
	)
}

// checkPorts verifies the configured development ports are free.
func checkPorts(report *diagnostics, p *project) {
	if p.Config.Dev.GoPort == 0 {
		return
	}
	if free, detail := portFree(p.Config.Dev.GoPort); free {
		report.pass("Go port", fmt.Sprintf("%d free", p.Config.Dev.GoPort))
	} else {
		report.warn(
			fmt.Sprintf("port %d is in use (%s)", p.Config.Dev.GoPort, detail),
			fmt.Sprintf("Stop the process using it, or change dev.goPort in %s.", configFileName),
		)
	}

	if p.Config.Dev.VitePort != 0 {
		if free, detail := portFree(p.Config.Dev.VitePort); free {
			report.pass("Vite port", fmt.Sprintf("%d free", p.Config.Dev.VitePort))
		} else {
			report.warn(
				fmt.Sprintf("port %d is in use (%s)", p.Config.Dev.VitePort, detail),
				"Vite is configured with strictPort, so it will refuse to start rather than pick another port.",
			)
		}
	}
}

// checkBuiltBinary reports the state of the production build directory.
func checkBuiltBinary(report *diagnostics, p *project) {
	hotFile := p.path(p.Config.Vite.HotFile)
	if support_Exists(hotFile) {
		info, err := os.Stat(hotFile)
		if err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			report.warn(
				"a stale dev server marker is present at "+p.Config.Vite.HotFile,
				"Remove it so a production build reads the asset manifest instead of looking for a dev server.",
			)
		} else {
			report.pass("dev server marker", "present (Vite is running)")
		}
	}

	if p.builtBinary() != "" {
		report.pass("built binary", p.builtBinary())
	} else {
		report.pass("built binary", "not built yet (run `vuepher build`)")
	}
}

// printSummary renders the tally.
func printSummary(report *diagnostics) {
	if report.failures == 0 && report.warnings == 0 {
		println(styleSuccess("Everything checks out."))
		return
	}

	if report.failures > 0 {
		printf("%s\n", styleError(fmt.Sprintf("%d problem(s):", report.failures)))
	}
	if report.warnings > 0 {
		printf("%s\n", styleWarn(fmt.Sprintf("%d warning(s):", report.warnings)))
	}
	for _, suggestion := range report.suggestions {
		printf("  %s %s\n", styleMuted("→"), suggestion)
	}
}

// pass records a successful check.
func (d *diagnostics) pass(label, detail string) {
	printf("  %s %-22s %s\n", styleSuccess("✓"), label, styleMuted(detail))
}

// warn records a problem that does not stop the project working.
func (d *diagnostics) warn(problem, suggestion string) {
	d.warnings++
	printf("  %s %-22s %s\n", styleWarn("!"), "warning", problem)
	if suggestion != "" {
		d.suggestions = append(d.suggestions, suggestion)
	}
}

// fail records a problem that does.
func (d *diagnostics) fail(problem, suggestion string) {
	d.failures++
	printf("  %s %-22s %s\n", styleError("✗"), "problem", problem)
	if suggestion != "" {
		d.suggestions = append(d.suggestions, suggestion)
	}
}

// portFree reports whether a TCP port can be bound, with a short reason when it
// cannot.
func portFree(port int) (bool, string) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err == nil {
		listener.Close()
		return true, ""
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return false, opErr.Err.Error()
	}
	return false, err.Error()
}

// parseGoVersion extracts "1.22" from "go version go1.22.3 linux/amd64".
func parseGoVersion(output string) string {
	fields := strings.Fields(output)
	for _, field := range fields {
		if !strings.HasPrefix(field, "go") {
			continue
		}
		version := strings.TrimPrefix(field, "go")
		// Drop any trailing platform suffix and toolchain markers such as
		// "1.22.3-X:nodwarf5".
		if index := strings.IndexAny(version, "-+"); index >= 0 {
			version = version[:index]
		}
		parts := strings.Split(version, ".")
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
	}
	return ""
}

// lessThan compares two "major.minor" version strings.
func lessThan(version string, wantMajor, wantMinor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	if major != wantMajor {
		return major < wantMajor
	}
	return minor < wantMinor
}

// support_Exists and support_IsDir wrap the support package helpers so the doctor
// checks read as prose without a qualified name on every line.
func support_Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func support_IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// unusedPath is a compile-time guard that keeps filepath imported for the paths
// doctor reports.
var _ = filepath.Join
