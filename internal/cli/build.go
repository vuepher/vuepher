package cli

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher/support"
)

// buildOptions collects the flags of `vuepher build`.
type buildOptions struct {
	out          string
	target       string
	allTargets   bool
	skipFrontend bool
	skipCompress bool
	debug        bool
}

func newBuildCommand() *cobra.Command {
	options := buildOptions{}

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build the frontend and compile a single deployable binary",
		Long: strings.TrimSpace(`
Runs the Vite production build, pre-compresses the assets, and compiles the Go
application with everything embedded.

The result is one file with no runtime dependencies. Node.js is needed to run this
command, never to run its output.

Steps:

  1. ` + "`npm run build`" + `     writes public/build with content-hashed filenames
  2. pre-compression       writes .br and .gz siblings for the asset handler
  3. ` + "`go build`" + `          embeds public/build and resources/views

Use --target to cross-compile,

  vuepher build --target linux/arm64

or --all to build every target listed in vuepher.json's build.targets.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(options)
		},
	}

	flags := cmd.Flags()
	flags.StringVarP(&options.out, "out", "o", "", "output path (default: build.out from vuepher.json)")
	flags.StringVar(&options.target, "target", "", "cross-compile for os/arch, e.g. linux/arm64")
	flags.BoolVar(&options.allTargets, "all", false, "build every target listed in vuepher.json")
	flags.BoolVar(&options.skipFrontend, "skip-frontend", false, "reuse the existing public/build")
	flags.BoolVar(&options.skipCompress, "skip-compress", false, "skip pre-compression")
	flags.BoolVar(&options.debug, "debug", false, "keep debug symbols and disable -s -w")
	return cmd
}

func runBuild(options buildOptions) error {
	project, err := openProject()
	if err != nil {
		return err
	}

	out := options.out
	if out == "" {
		out = project.Config.Build.Out
	}

	if !options.skipFrontend {
		if err := buildFrontend(project); err != nil {
			return err
		}
	} else if !support.IsDir(project.path(project.Config.Vite.BuildDir)) {
		return fmt.Errorf(
			"vuepher: --skip-frontend was given but %s does not exist.\n"+
				"  Run `npm run build` first, or drop the flag.",
			project.Config.Vite.BuildDir,
		)
	}

	if !options.skipCompress && project.Config.Build.Compress {
		if err := compressAssets(project); err != nil {
			// Compression is an optimisation: a failure must not stop a build
			// that would otherwise produce a working binary.
			printWarning("could not pre-compress the assets: %v", err)
		}
	}

	targets := resolveTargets(project, options)
	for _, target := range targets {
		if err := compileBinary(project, out, target, options.debug); err != nil {
			return err
		}
	}

	printBuildSummary(project, out, targets)
	return nil
}

// buildFrontend runs the Vite production build.
func buildFrontend(project *project) error {
	if _, ok := commandExists("npm"); !ok {
		return fmt.Errorf(
			"vuepher: npm was not found on PATH.\n"+
				"  Node.js is required to build the frontend. Install it from https://nodejs.org, "+
				"or run `vuepher build --skip-frontend` to reuse the existing %s.",
			project.Config.Vite.BuildDir,
		)
	}
	if !support.IsDir(project.path("node_modules")) {
		return fmt.Errorf(
			"vuepher: %s is missing.\n"+
				"  Run `npm install` in %s, then build again.",
			"node_modules", project.Root,
		)
	}

	printStep("building the frontend")
	if output, err := runIn(project.Root, "npm", "run", "build"); err != nil {
		return fmt.Errorf("vuepher: the frontend build failed:\n%s", indent(output))
	}
	return nil
}

// compressAssets writes .br and .gz siblings for every text asset. The asset
// handler prefers them when the client accepts the encoding, so the work is done
// once at build time instead of per request (R8).
func compressAssets(project *project) error {
	buildDir := project.path(project.Config.Vite.BuildDir)
	if !support.IsDir(buildDir) {
		return nil
	}

	compressed := 0
	err := filepath.WalkDir(buildDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if !isCompressible(path) {
			return nil
		}
		// Never compress an already-compressed variant.
		if strings.HasSuffix(path, ".gz") || strings.HasSuffix(path, ".br") {
			return nil
		}

		if err := compressFile(path); err != nil {
			return err
		}
		compressed++
		return nil
	})
	if err != nil {
		return err
	}

	if compressed > 0 {
		printStep("pre-compressed %d asset(s) with brotli and gzip", compressed)
	}
	return nil
}

// compressFile writes the .br and .gz siblings of one asset.
//
// It is separate from the walk so that the compression itself can be tested: the
// failure mode of a compressor that is not flushed is a plausible-looking empty
// file, which is worth a test rather than a code review.
func compressFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Below a few hundred bytes the framing overhead outweighs the saving.
	if len(content) < 512 {
		return nil
	}

	// The brotli writer has to be closed before its output is read: it buffers the
	// final block, so a writer that is never closed yields an empty file. An empty
	// .br is worse than no .br at all — the asset handler prefers it for every
	// browser that accepts brotli, which is all of them, so the application would
	// be served zero-byte JavaScript.
	var brotliBuf bytes.Buffer
	brotliWriter := brotli.NewWriterLevel(&brotliBuf, brotli.DefaultCompression)
	if _, err := brotliWriter.Write(content); err != nil {
		return err
	}
	if err := brotliWriter.Close(); err != nil {
		return err
	}
	if err := writeCompressed(path+".br", brotliBuf.Bytes(), content); err != nil {
		return err
	}

	var gzipBuf bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&gzipBuf, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := gzipWriter.Write(content); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	return writeCompressed(path+".gz", gzipBuf.Bytes(), content)
}

// writeCompressed writes a compressed variant, skipping it when it is not
// actually smaller.
//
// An empty result is an error rather than a file: it means the compressor did
// not flush, and writing it would hand browsers a zero-byte asset while the build
// still reports success.
func writeCompressed(target string, compressed, original []byte) error {
	if len(compressed) == 0 {
		return fmt.Errorf("vuepher: compression produced an empty file for %s", target)
	}
	if len(compressed) >= len(original) {
		return nil
	}
	return os.WriteFile(target, compressed, 0o644)
}

// isCompressible reports whether a file is worth compressing.
func isCompressible(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".css", ".html", ".json", ".svg", ".map", ".txt", ".xml", ".wasm":
		return true
	default:
		return false
	}
}

// resolveTargets determines which platform builds to produce.
func resolveTargets(project *project, options buildOptions) []string {
	if options.allTargets {
		if len(project.Config.Build.Targets) == 0 {
			return []string{runtime.GOOS + "/" + runtime.GOARCH}
		}
		return project.Config.Build.Targets
	}
	if options.target != "" {
		return []string{options.target}
	}
	return []string{runtime.GOOS + "/" + runtime.GOARCH}
}

// compileBinary runs go build for one platform.
func compileBinary(project *project, out, target string, debug bool) error {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf(
			"vuepher: --target %q is not in os/arch form; try linux/amd64 or darwin/arm64",
			target,
		)
	}
	goos, goarch := parts[0], parts[1]

	outputPath := out
	if len(project.Config.Build.Targets) > 1 || goos != runtime.GOOS || goarch != runtime.GOARCH {
		outputPath = fmt.Sprintf("%s-%s-%s", out, goos, goarch)
	}
	if goos == "windows" {
		outputPath += ".exe"
	}

	args := []string{"build", "-o", outputPath}

	ldflags := project.Config.Build.Ldflags
	if debug {
		ldflags = nil
	}
	if len(ldflags) > 0 {
		args = append(args, "-ldflags", strings.Join(ldflags, " "))
	}

	entry := project.Config.Entry
	if entry == "" {
		entry = "."
	}
	args = append(args, entry)

	printStep("compiling %s", styleInfo(target))

	cmd := exec.Command("go", args...)
	cmd.Dir = project.Root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")

	// CGO is disabled so the result is a static binary that runs on a bare
	// container. That is also why the sqlite driver is a pure-Go one: the
	// conventional cgo driver would make this impossible without a cross
	// toolchain.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"vuepher: the build failed for %s:\n%s",
			target, indent(string(output)),
		)
	}

	info, err := os.Stat(project.path(outputPath))
	if err != nil {
		return fmt.Errorf("vuepher: the build reported success but %s is missing", outputPath)
	}
	printCreated(fmt.Sprintf("%s %s", outputPath, styleMuted("("+humanSize(info.Size())+")")))
	return nil
}

// printBuildSummary reports the artefacts and their sizes.
func printBuildSummary(project *project, out string, targets []string) {
	println()
	println(styleSuccess("Build complete."))
	println()
	for _, target := range targets {
		path := out
		if len(project.Config.Build.Targets) > 1 {
			parts := strings.SplitN(target, "/", 2)
			path = fmt.Sprintf("%s-%s-%s", out, parts[0], parts[1])
			if parts[0] == "windows" {
				path += ".exe"
			}
		}
		info, err := os.Stat(project.path(path))
		if err != nil {
			continue
		}
		printBullet(target, humanSize(info.Size()))
	}
	println()
	printf("  Deploy the binary and run it. Node.js is not needed at runtime.\n")
	println()
}

// humanSize renders a byte count for humans.
func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value := float64(bytes)
	units := []string{"KiB", "MiB", "GiB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/unit)
}
