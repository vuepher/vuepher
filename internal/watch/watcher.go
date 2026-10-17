// Package watch provides the debounced filesystem watcher behind `vuepher dev`
// and incremental type generation (R2, TG-06).
//
// It exists as its own package because the two consumers want different
// behaviour from the same events: the Go rebuild wants to coalesce a burst of
// writes from an editor into one restart, while type generation wants to run on a
// single changed file. A shared watcher with a debounce window covers both.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Options configure a Watcher.
type Options struct {
	// Root is the directory tree to watch.
	Root string

	// Patterns are the globs that select interesting files, relative to Root:
	//
	//	app/**/*.go
	//	*.go
	//
	// An empty list watches everything not ignored.
	Patterns []string

	// Ignore lists paths that are never watched. Entries are matched as path
	// prefixes, so "storage" excludes the whole directory.
	Ignore []string

	// Debounce is how long to wait after the last event before reporting a
	// batch. Editors can fire several events for one save.
	Debounce time.Duration

	// OnChange receives the paths that changed. It is called from the watcher's
	// goroutine, so it must not block for long.
	OnChange func(paths []string)

	// Logger receives diagnostic messages.
	Logger *slog.Logger
}

// Watcher reports filesystem changes under a root directory.
type Watcher struct {
	opts    Options
	watcher *fsnotify.Watcher
	logger  *slog.Logger

	mu      sync.Mutex
	pending map[string]struct{}
	timer   *time.Timer
}

// New builds a watcher.
func New(opts Options) (*Watcher, error) {
	if opts.Root == "" {
		return nil, errors.New("watch: Root is required")
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 250 * time.Millisecond
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: could not create the filesystem watcher: %w", err)
	}

	w := &Watcher{
		opts:    opts,
		watcher: inner,
		logger:  opts.Logger,
		pending: map[string]struct{}{},
	}
	if err := w.addDirs(opts.Root); err != nil {
		inner.Close()
		return nil, err
	}
	return w, nil
}

// addDirs registers every directory under root that is not ignored. fsnotify is
// not recursive, so each directory has to be added individually — and a new
// directory has to be added when it appears, which Run handles.
func (w *Watcher) addDirs(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory should not abort the walk; a project with
			// one protected folder is still watchable.
			w.logger.Debug("watch: skipping unreadable path", "path", path, "error", err)
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if w.ignored(path) {
			return fs.SkipDir
		}
		if err := w.watcher.Add(path); err != nil {
			w.logger.Debug("watch: could not watch directory", "path", path, "error", err)
		}
		return nil
	})
}

// ignored reports whether a path is excluded.
func (w *Watcher) ignored(path string) bool {
	// The hidden directories that exist in every project are never interesting
	// and watching .git produces a constant stream of noise.
	base := filepath.Base(path)
	if base == ".git" || base == "node_modules" {
		return true
	}

	relative, err := filepath.Rel(w.opts.Root, path)
	if err != nil {
		relative = path
	}
	relative = filepath.ToSlash(relative)

	for _, pattern := range w.opts.Ignore {
		pattern = strings.Trim(pattern, "/")
		if pattern == "" {
			continue
		}
		if relative == pattern || strings.HasPrefix(relative, pattern+"/") {
			return true
		}
		// Support the common "**/name/**" form from vuepher.json.
		if strings.Contains(pattern, "**") {
			needle := strings.Trim(pattern, "*/")
			if needle != "" && (relative == needle || strings.HasPrefix(relative, needle+"/") ||
				strings.Contains(relative, "/"+needle+"/")) {
				return true
			}
		}
	}
	return false
}

// interesting reports whether a changed file matches the watch patterns.
func (w *Watcher) interesting(path string) bool {
	if w.ignored(path) {
		return false
	}
	if len(w.opts.Patterns) == 0 {
		return true
	}

	relative, err := filepath.Rel(w.opts.Root, path)
	if err != nil {
		relative = path
	}
	relative = filepath.ToSlash(relative)

	for _, pattern := range w.opts.Patterns {
		if Match(pattern, relative) {
			return true
		}
	}
	return false
}

// Run watches until the context is cancelled or Close is called.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.watcher.Close()

	for {
		select {
		case <-ctx.Done():
			w.flush()
			return nil
		case event, ok := <-w.watcher.Events:
			if !ok {
				w.flush()
				return nil
			}
			w.handle(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				continue
			}
			// Limits on inotify watch descriptors are common on Linux and worth
			// naming explicitly, because the symptom is otherwise just "changes
			// stop being detected".
			w.logger.Warn("watch: filesystem watcher error", "error", err)
		}
	}
}

// handle processes one event, coalescing bursts.
func (w *Watcher) handle(event fsnotify.Event) {
	if event.Op&(fsnotify.Create) != 0 {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			// A new directory: watch it so files created inside are seen.
			if !w.ignored(event.Name) {
				_ = w.addDirs(event.Name)
			}
			return
		}
	}

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		return
	}
	if !w.interesting(event.Name) {
		return
	}

	w.mu.Lock()
	w.pending[event.Name] = struct{}{}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.opts.Debounce, w.flush)
	w.mu.Unlock()
}

// flush reports the accumulated paths.
func (w *Watcher) flush() {
	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	paths := make([]string, 0, len(w.pending))
	for path := range w.pending {
		paths = append(paths, path)
	}
	w.pending = map[string]struct{}{}
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()

	if w.opts.OnChange != nil {
		w.opts.OnChange(paths)
	}
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()
	return w.watcher.Close()
}

// Match implements the glob dialect used in vuepher.json. It supports `*` within
// a segment and `**` across segments, matching how developers expect a watch
// pattern to behave:
//
//	app/**/*.go    every .go file under app, at any depth
//	resources/*.css  .css files directly in resources
//	*.go           .go files at the root only
func Match(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)

	if !strings.Contains(pattern, "**") {
		matched, err := filepath.Match(pattern, name)
		return err == nil && matched
	}

	// Split on the first "**" and require the prefix to match and the suffix to
	// match the remaining path (or any part of it).
	index := strings.Index(pattern, "**")
	prefix := strings.TrimSuffix(pattern[:index], "/")
	suffix := strings.TrimPrefix(pattern[index+2:], "/")

	if prefix != "" {
		if name != prefix && !strings.HasPrefix(name, prefix+"/") {
			return false
		}
		name = strings.TrimPrefix(name, prefix)
		name = strings.TrimPrefix(name, "/")
	}
	if suffix == "" {
		return true
	}

	// The suffix may match the whole remaining path or just its tail, so that
	// `app/**/*.go` matches `app/models/post.go` as well as `app/post.go`.
	segments := strings.Split(name, "/")
	for i := range segments {
		candidate := strings.Join(segments[i:], "/")
		if matched, err := filepath.Match(suffix, candidate); err == nil && matched {
			return true
		}
	}
	return false
}
