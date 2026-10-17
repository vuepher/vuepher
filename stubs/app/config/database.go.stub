package config

import (
	"os"
	"path/filepath"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/orm"
)

// Database maps the framework's typed configuration onto the ORM connection
// configuration (PRD §8 keeps the mapping in config/ so that the framework
// never has to know about application-specific settings).
func Database(app *vuepher.App) (orm.Config, error) {
	cfg := app.Config

	dsn, err := cfg.Database.DSN()
	if err != nil {
		return orm.Config{}, err
	}

	return orm.Config{
		Driver:       cfg.Database.Connection,
		DSN:          dsn,
		MaxOpenConns: cfg.Database.MaxOpenConns,
		MaxIdleConns: cfg.Database.MaxIdleConns,
		LogLevel:     cfg.Database.LogLevel,
		// Statement logging is controlled by DB_LOG_LEVEL, not by APP_DEBUG:
		// APP_DEBUG is on for every local project and enabling SQL logging there
		// buries the application's own output under a query per request.
		// Set DB_LOG_LEVEL=info (or debug) when you actually want to see SQL.
		Debug:  false,
		Logger: app.Logger,
	}, nil
}

// EnsureDatabaseDirectory creates the parent directory of a file-based database
// so that the first run of a fresh project does not fail with "unable to open
// database file".
func EnsureDatabaseDirectory(app *vuepher.App) error {
	cfg := app.Config
	switch cfg.Database.Connection {
	case "sqlite", "sqlite3", "":
	default:
		// Hosted databases do not need a local directory.
		return nil
	}
	database := cfg.Database.Database
	if database == "" || database == ":memory:" {
		return nil
	}
	if !filepath.IsAbs(database) {
		database = filepath.Join(cfg.BasePath, database)
	}
	return os.MkdirAll(filepath.Dir(database), 0o755)
}
