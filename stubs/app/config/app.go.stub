// Package config turns environment configuration into the typed values the rest
// of the application consumes.
//
// The framework loads and validates `.env` (see vuepher.LoadConfig); this package
// is where application-specific decisions live, so that framework code never has
// to know about them.
package config

import (
	"time"

	"github.com/vuepher/vuepher"
)

// Application-specific settings that the framework has no opinion about. Add
// yours here and read them in a handler through ctx.App().
const (
	// PaginationSize is the default page size for list screens.
	PaginationSize = 15

	// Timeout for an outbound HTTP call made from a handler.
	OutboundTimeout = 10 * time.Second
)

// App exposes the application block of the configuration with the conveniences
// the application needs.
type App struct {
	vuepher.AppConfig
}

// New wraps the framework configuration.
func New(cfg *vuepher.Config) App {
	return App{AppConfig: cfg.App}
}

// IsDebug reports whether detailed errors should be shown to the user. It reads
// the framework value so there is exactly one source of truth.
func (a App) IsDebug() bool { return a.AppConfig.Debug }

// Locale returns the active locale for localisable strings, defaulting to
// English.
func (a App) Locale() string {
	if a.AppConfig.Locale == "" {
		return "en"
	}
	return a.AppConfig.Locale
}
