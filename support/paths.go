package support

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MarkerFiles are the files that identify a Vuepher project root, in the order
// they are looked for.
var MarkerFiles = []string{"vuepher.json", "go.mod"}

// ErrProjectRootNotFound is returned when a command that needs a project root is
// run outside a project.
var ErrProjectRootNotFound = errors.New(
	"vuepher: no project found in this directory or any parent. " +
		"Run `vuepher new <project>` to create one, or run this command from inside a project",
)

// ProjectRoot walks up from dir looking for a marker file. It returns the
// absolute path of the project root.
//
// Walking up matters for developer experience: running `vuepher make:controller`
// from a nested directory inside the project should work.
func ProjectRoot(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	current := abs
	for {
		for _, marker := range MarkerFiles {
			if Exists(filepath.Join(current, marker)) {
				return current, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrProjectRootNotFound
		}
		current = parent
	}
}

// Exists reports whether a path exists, following symlinks.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether a path exists and is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsEmptyDir reports whether a directory exists and contains nothing.
func IsEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// EnsureDir creates a directory and any missing parents.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("vuepher: could not create directory %s: %w", path, err)
	}
	return nil
}

// ErrFileExists is returned when writing would clobber an existing file and the
// force flag is not set. Generators report it so the user knows to pass --force
// rather than silently losing work.
var ErrFileExists = errors.New("vuepher: file already exists")

// WriteFile writes a file, creating parent directories. Existing files are kept
// unless force is set, which is what stops a generator from overwriting manual
// edits.
func WriteFile(path string, content []byte, force bool) error {
	if !force && Exists(path) {
		return fmt.Errorf("%w: %s", ErrFileExists, path)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if strings.HasSuffix(path, ".sh") || strings.HasSuffix(path, ".command") {
		mode = 0o755
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return fmt.Errorf("vuepher: could not write %s: %w", path, err)
	}
	return nil
}

// ReadFile reads a file from the working tree.
func ReadFile(path string) ([]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vuepher: could not read %s: %w", path, err)
	}
	return content, nil
}

// CopyFile copies one file, creating parents.
func CopyFile(src, dst string, force bool) error {
	if !force && Exists(dst) {
		return fmt.Errorf("%w: %s", ErrFileExists, dst)
	}
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := EnsureDir(filepath.Dir(dst)); err != nil {
		return err
	}
	return os.WriteFile(dst, content, info.Mode().Perm())
}

// CopyDir copies a directory tree from the OS filesystem, skipping paths for
// which skip returns true.
func CopyDir(src, dst string, force bool, skip func(string) bool) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skip != nil && skip(path) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			return EnsureDir(target)
		}
		return CopyFile(path, target, force)
	})
}

// RelativeTo returns path expressed relative to base, falling back to path when
// it is outside base. Used for the short paths in CLI output.
func RelativeTo(base, path string) string {
	relative, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return relative
}

// Touch creates an empty file when it does not exist, used for .gitkeep
// placeholders in directories that must survive a clone.
func Touch(path string) error {
	if Exists(path) {
		return nil
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return file.Close()
}

// ReadAll reads everything from a reader, with a short wrapper so callers do not
// need the io import for one call.
func ReadAll(reader io.Reader) ([]byte, error) {
	return io.ReadAll(reader)
}
