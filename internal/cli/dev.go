package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vuepher/vuepher/internal/procman"
	"github.com/vuepher/vuepher/internal/typegen"
	"github.com/vuepher/vuepher/internal/watch"
	"github.com/vuepher/vuepher/support"
)

// devOptions collects the flags of `vuepher dev`.
type devOptions struct {
	goPort   int
	vitePort int
	noVite   bool
	noReload bool
}

func newDevCommand() *cobra.Command {
	options := devOptions{}

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Run the Go server and the Vite dev server together",
		Long: strings.TrimSpace(`
Starts both halves of the application in one terminal:

  go    the HTTP server, rebuilt and restarted when a .go file changes
  vite  the frontend dev server, with hot module replacement

They run as children of a single supervisor, so Ctrl+C stops both and leaves no
process holding a port. Restarting the Go server does not touch Vite, which is what
keeps the HMR websocket alive while you edit backend code.

Types are regenerated whenever a model, request or controller changes, so the Vue
side sees new props without a manual step.

Ports come from vuepher.json and can be overridden here.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDev(options)
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&options.goPort, "port", 0, "port for the Go server (default: dev.goPort)")
	flags.IntVar(&options.vitePort, "vite-port", 0, "port for the Vite dev server (default: dev.vitePort)")
	flags.BoolVar(&options.noVite, "no-vite", false, "run only the Go server")
	flags.BoolVar(&options.noReload, "no-reload", false, "do not restart the Go server on changes")
	return cmd
}

func runDev(options devOptions) error {
	project, err := openProject()
	if err != nil {
		return err
	}
	if err := project.requireModule("dev"); err != nil {
		return err
	}

	goPort := options.goPort
	if goPort == 0 {
		goPort = project.Config.Dev.GoPort
	}
	vitePort := options.vitePort
	if vitePort == 0 {
		vitePort = project.Config.Dev.VitePort
	}
	if goPort == vitePort {
		return fmt.Errorf(
			"vuepher: the Go server and Vite cannot share port %d; pass --port or --vite-port",
			goPort,
		)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Signal handling: Ctrl+C must stop both children, and a second Ctrl+C must
	// not be necessary.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	printDevBanner(project, goPort, vitePort, options)

	// The hot file is how the Go process knows a dev server is running: the
	// renderer reads it at boot and emits Vite client tags instead of reading the
	// build manifest (VIEW-03).
	hotPath := project.path(project.Config.Vite.HotFile)
	if !options.noVite {
		if err := writeHotFile(hotPath, vitePort); err != nil {
			return err
		}
		// Removing it on exit matters: leaving it behind makes the next
		// production run load assets from a dev server that is not there.
		defer func() {
			if err := os.Remove(hotPath); err != nil && !os.IsNotExist(err) {
				logger.Debug("could not remove the hot file", "error", err)
			}
		}()
	}

	restarts := make(chan struct{}, 1)
	requestRestart := func() {
		select {
		case restarts <- struct{}{}:
		default:
			// A restart is already queued; several files changing at once should
			// still produce one restart.
		}
	}

	var watcher *watch.Watcher
	if !options.noReload {
		debounce := time.Duration(project.Config.Dev.DebounceMs) * time.Millisecond
		watcher, err = watch.New(watch.Options{
			Root:     project.Root,
			Patterns: project.Config.Dev.Watch,
			Ignore:   project.Config.Dev.Ignore,
			Debounce: debounce,
			Logger:   logger,
			OnChange: func(paths []string) {
				regenerateTypes(project, paths, logger)
				requestRestart()
			},
		})
		if err != nil {
			return err
		}
		defer watcher.Close()
	}

	goEnv := []string{
		fmt.Sprintf("APP_URL=http://localhost:%d", goPort),
		fmt.Sprintf("VITE_DEV_SERVER=http://localhost:%d", vitePort),
		fmt.Sprintf("VITE_PORT=%d", vitePort),
		"APP_ENV=local",
	}

	goServer := procman.NewRestarting(procman.Process{
		Name:    "go",
		Color:   "cyan",
		Command: "go",
		Args:    []string{"run", "."},
		Dir:     project.Root,
		Env:     goEnv,
	}, logger, os.Stdout)

	errCh := make(chan error, 3)

	if !options.noVite {
		if err := ensureNodeModules(project); err != nil {
			return err
		}
		vite := procman.New(logger, os.Stdout, procman.Process{
			Name:    "vite",
			Color:   "magenta",
			Command: "npm",
			Args:    []string{"run", "dev", "--", "--port", itoa(vitePort), "--strictPort"},
			Dir:     project.Root,
			Env:     []string{fmt.Sprintf("VITE_PORT=%d", vitePort)},
		})
		go func() { errCh <- vite.Run(ctx) }()
	}

	go func() { errCh <- goServer.Run(ctx, restarts) }()

	if watcher != nil {
		go func() { errCh <- watcher.Run(ctx) }()
	}

	select {
	case <-ctx.Done():
		println()
		println(styleMuted("Shutting down…"))
		return nil
	case err := <-errCh:
		// A child that fails to start (missing binary) ends the session; a child
		// that merely exits is handled by the restarting supervisor.
		if err != nil {
			stop()
			return err
		}
		stop()
		return nil
	}
}

// regenerateTypes runs type generation when a file whose Go type the frontend
// depends on changed (TG-06). It is best-effort: a broken file mid-edit must not
// stop the dev server.
func regenerateTypes(project *project, paths []string, logger *slog.Logger) {
	if !project.Config.Types.Watch {
		return
	}
	if !anyGoTypeSource(project, paths) {
		return
	}

	started := time.Now()
	result, err := typegen.Generate(typegen.Options{
		Root:    project.Root,
		Sources: project.Config.Types.Source,
		Out:     project.Config.Types.Out,
	})
	if err != nil {
		logger.Debug("could not regenerate types", "error", err)
		return
	}
	for _, warning := range result.Warnings {
		logger.Debug("type generation", "warning", warning)
	}
	// The target is under one second (TG-06); reporting the duration makes a
	// regression visible rather than invisible.
	printf("  %s types regenerated in %s\n",
		styleInfo("•"), styleMuted(time.Since(started).Round(time.Millisecond).String()))
}

// anyGoTypeSource reports whether any changed path feeds type generation.
func anyGoTypeSource(project *project, paths []string) bool {
	for _, path := range paths {
		relative := support.RelativeTo(project.Root, path)
		for _, source := range project.Config.Types.Source {
			source = filepath.ToSlash(strings.TrimSuffix(source, "/"))
			if strings.HasPrefix(filepath.ToSlash(relative), source+"/") {
				return true
			}
		}
	}
	return false
}

// ensureNodeModules gives a clear error rather than letting npm fail obscurely.
func ensureNodeModules(project *project) error {
	if support.IsDir(project.path("node_modules")) {
		return nil
	}
	return fmt.Errorf(
		"vuepher: node_modules is missing.\n"+
			"  Run `npm install` in %s first, or pass --no-vite to run only the Go server.",
		project.Root,
	)
}

// writeHotFile records that a dev server is running, and on which port.
func writeHotFile(path string, port int) error {
	if err := support.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	content := fmt.Sprintf("http://localhost:%d\n", port)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("vuepher: could not write the dev server marker %s: %w", path, err)
	}
	return nil
}

// printDevBanner tells the user where to look.
func printDevBanner(project *project, goPort, vitePort int, options devOptions) {
	println()
	printf("  %s\n", styleHeading(project.Config.Entry+" · "+filepath.Base(project.Root)))
	printf("  %s %s\n", styleMuted("app    "), styleInfo(fmt.Sprintf("http://localhost:%d", goPort)))
	if !options.noVite {
		printf("  %s %s\n", styleMuted("vite   "), styleInfo(fmt.Sprintf("http://localhost:%d", vitePort)))
	}
	if !options.noReload {
		printf("  %s %s\n", styleMuted("watching"), styleMuted(strings.Join(project.Config.Dev.Watch, " ")))
	}
	printf("  %s\n", styleMuted("Ctrl+C to stop"))
	println()
}
