package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// projectConfig mirrors vuepher.json. Every field has a default, so a project
// with no configuration file at all still works: the file exists to change
// defaults, not to enable the tool.
type projectConfig struct {
	Entry string      `json:"entry"`
	Dev   devConfig   `json:"dev"`
	Build buildConfig `json:"build"`
	Types typesConfig `json:"types"`
	Paths pathsConfig `json:"paths"`
	Vite  viteConfig  `json:"vite"`
}

// viteConfig describes the frontend build layout. The defaults match what
// `vuepher new` writes and what the renderer expects, so the section only needs
// to appear in vuepher.json when a project moves something.
type viteConfig struct {
	// DevServer is the Vite dev server origin used while `vuepher dev` runs.
	DevServer string `json:"devServer"`

	// BuildDir is where Vite writes production assets.
	BuildDir string `json:"buildDir"`

	// ManifestPath is the Vite manifest, read to emit asset tags in production.
	ManifestPath string `json:"manifestPath"`

	// HotFile is the marker `vuepher dev` writes while the dev server is up. Its
	// presence is how the application knows to emit Vite client tags.
	HotFile string `json:"hotFile"`

	// EntryPoint is the Vite entry, relative to the project root.
	EntryPoint string `json:"entryPoint"`
}

// devConfig configures `vuepher dev`.
type devConfig struct {
	GoPort          int      `json:"goPort"`
	VitePort        int      `json:"vitePort"`
	Host            string   `json:"host"`
	Watch           []string `json:"watch"`
	Ignore          []string `json:"ignore"`
	DebounceMs      int      `json:"debounceMs"`
	ReloadTimeoutMs int      `json:"reloadTimeoutMs"`
}

// buildConfig configures `vuepher build`.
type buildConfig struct {
	Out      string   `json:"out"`
	EmbedDir string   `json:"embedDir"`
	Compress bool     `json:"compress"`
	Ldflags  []string `json:"ldflags"`
	Targets  []string `json:"targets"`
	// Strip enables -ldflags="-s -w". It is on by default because the PRD's
	// binary-size target (§7.2) assumes it.
	Strip bool `json:"strip"`
}

// typesConfig configures `vuepher types:generate`.
type typesConfig struct {
	Source []string `json:"source"`
	Out    string   `json:"out"`
	Watch  bool     `json:"watch"`
	Check  bool     `json:"check"`
}

// pathsConfig lets a project relocate the directories the CLI knows about.
type pathsConfig struct {
	Views      string `json:"views"`
	Pages      string `json:"pages"`
	Stubs      string `json:"stubs"`
	Migrations string `json:"migrations"`
}

// configFileName is the file the CLI looks for at the project root. It is also
// one of the markers support.ProjectRoot uses to find the root.
const configFileName = "vuepher.json"

// defaultConfig returns the built-in defaults.
func defaultConfig() *projectConfig {
	return &projectConfig{
		Entry: "main.go",
		Dev: devConfig{
			GoPort:   8080,
			VitePort: 5173,
			Host:     "localhost",
			Watch: []string{
				"app/**/*.go",
				"routes/**/*.go",
				"config/**/*.go",
				"bootstrap/**/*.go",
				"database/**/*.go",
				"resources/views/**/*.html",
				"*.go",
			},
			Ignore: []string{
				"storage",
				"public/build",
				"resources/js/types/generated",
				"node_modules",
				"tests",
			},
			DebounceMs:      250,
			ReloadTimeoutMs: 2000,
		},
		Build: buildConfig{
			Out:      filepath.Join("build", "vuepher-app"),
			EmbedDir: filepath.Join("public", "build"),
			Compress: true,
			Ldflags:  []string{"-s", "-w"},
			Strip:    true,
			Targets:  []string{"linux/amd64", "linux/arm64", "darwin/arm64", "windows/amd64"},
		},
		Types: typesConfig{
			Source: []string{
				filepath.Join("app", "models"),
				filepath.Join("app", "requests"),
				filepath.Join("app", "controllers"),
			},
			Out:   filepath.Join("resources", "js", "types", "generated"),
			Watch: true,
		},
		Paths: pathsConfig{
			Views:      filepath.Join("resources", "views"),
			Pages:      filepath.Join("resources", "js", "Pages"),
			Stubs:      "stubs",
			Migrations: filepath.Join("database", "migrations"),
		},
		Vite: viteConfig{
			DevServer:    "http://localhost:5173",
			BuildDir:     filepath.Join("public", "build"),
			ManifestPath: filepath.Join("public", "build", ".vite", "manifest.json"),
			HotFile:      filepath.Join("public", "hot"),
			EntryPoint:   filepath.Join("resources", "js", "app.ts"),
		},
	}
}

// loadProjectConfig reads vuepher.json from a project root, layering it over the
// defaults.
//
// A malformed file is reported with the field that failed and the line number
// where possible, because "invalid JSON" alone sends people hunting.
func loadProjectConfig(root string) (*projectConfig, error) {
	cfg := defaultConfig()

	path := filepath.Join(root, configFileName)
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("vuepher: could not read %s: %w", configFileName, err)
	}

	if err := json.Unmarshal(content, cfg); err != nil {
		return nil, fmt.Errorf(
			"vuepher: %s is not valid JSON: %w\n"+
				"  Fix the file, or delete it to use the defaults.",
			configFileName, err,
		)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// validate rejects settings that are individually plausible but do not work
// together, naming the fix rather than the symptom.
func (c *projectConfig) validate() error {
	if c.Dev.GoPort != 0 && c.Dev.GoPort == c.Dev.VitePort {
		return fmt.Errorf(
			"vuepher: dev.goPort and dev.vitePort are both %d in %s.\n"+
				"  They must differ: the Go server and the Vite dev server are separate processes.",
			c.Dev.GoPort, configFileName,
		)
	}
	if c.Dev.GoPort < 0 || c.Dev.GoPort > 65535 {
		return fmt.Errorf("vuepher: dev.goPort %d is not a valid port", c.Dev.GoPort)
	}
	if c.Dev.VitePort < 0 || c.Dev.VitePort > 65535 {
		return fmt.Errorf("vuepher: dev.vitePort %d is not a valid port", c.Dev.VitePort)
	}
	return nil
}
