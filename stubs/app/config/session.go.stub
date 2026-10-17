package config

import (
	"github.com/vuepher/vuepher"
)

// Session returns the session configuration.
//
// The value comes straight from the framework configuration, which reads it from
// .env. This function exists so that bootstrap reads configuration through the
// config package consistently, and so a project that needs a different store per
// environment has one obvious place to change it.
//
// Supported drivers:
//
//	cookie    encrypted, authenticated cookie; no storage, survives restarts
//	memory    in-process; convenient in tests, sessions are lost on restart
//
// Session drivers backed by the database or Redis land in Phase 2. Until then
// the framework falls back to cookies and logs a warning naming the driver.
func Session(app *vuepher.App) vuepher.SessionConfig {
	cfg := app.Config.Session
	if cfg.LifetimeMinutes <= 0 {
		cfg.LifetimeMinutes = 120
	}
	if cfg.Name == "" {
		cfg.Name = "vuepher_session"
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	return cfg
}
