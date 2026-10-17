package config

import (
	"github.com/vuepher/vuepher"
)

// Vite returns the frontend build configuration (VIEW-03).
//
// Paths are relative to the project root, which is the layout the renderer
// expects: it resolves them against the same filesystem it reads app.html from,
// so a built binary and a development server read the same structure.
func Vite(app *vuepher.App) vuepher.ViteConfig {
	cfg := app.Config.Vite
	if cfg.EntryPoint == "" {
		cfg.EntryPoint = "resources/js/app.ts"
	}
	if cfg.BuildDir == "" {
		cfg.BuildDir = "public/build"
	}
	if cfg.ManifestPath == "" {
		cfg.ManifestPath = "public/build/.vite/manifest.json"
	}
	return cfg
}

// IsDevServerRunning reports whether the frontend is served by the Vite dev
// server. The framework sets this when public/hot exists, which `vuepher dev`
// writes while it is up.
func IsDevServerRunning(app *vuepher.App) bool {
	return app.Config.Vite.UseDevServer
}
