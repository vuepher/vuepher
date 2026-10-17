// Package orm is Vuepher's thin, opt-in wrapper around GORM (DB-01 … DB-10).
//
// The design principle from the PRD is that this package must never lock you in:
// it adds ergonomics — a fluent query builder, a paginator shaped for the
// frontend, versioned migrations — while the raw *gorm.DB stays reachable at
// every point through DB.Gorm() or the embedded field.
//
// The wrapper covers the common 90% of queries. When a query needs something
// GORM does better (complex joins, raw SQL, custom dialectors), drop down to
// GORM directly and keep using the wrapper elsewhere in the same codebase.
package orm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB wraps a *gorm.DB with the connection metadata Vuepher needs (pool tuning,
// driver name, logger) while still embedding the original so every GORM method
// remains available:
//
//	db.Where("published = ?", true).Find(&posts)  // raw GORM, still fine
type DB struct {
	*gorm.DB

	driver string
	logger *slog.Logger
	debug  bool
}

// Config describes a database connection (DB-01).
type Config struct {
	// Driver is "sqlite", "postgres" or "mysql", matching DB_CONNECTION.
	Driver string

	// DSN is the connection string. Config.DatabaseConfig.DSN() builds it.
	DSN string

	// MaxOpenConns and MaxIdleConns tune the pool. Zero means the driver
	// default.
	MaxOpenConns int
	MaxIdleConns int

	// LogLevel is one of "silent", "error", "warn", "info". Anything else is
	// treated as "warn".
	LogLevel string

	// Debug logs every statement, including the successful ones. It is intended
	// to be driven by DB_LOG_LEVEL, not by APP_DEBUG: a local project has debug
	// mode on by default, and logging every query there buries the application's
	// output. Never enable it in production.
	Debug bool

	// Logger receives the framework's own diagnostics.
	Logger *slog.Logger

	// Now allows tests to control the clock used for slow-query reporting.
	Now func() time.Time
}

// Connect opens a database connection and configures the pool (DB-01).
func Connect(cfg Config) (*DB, error) {
	if cfg.DSN == "" {
		return nil, errors.New("vuepher/orm: DSN is empty; check DB_CONNECTION and DB_DATABASE")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	gormCfg := &gorm.Config{
		Logger: newGormLogger(cfg),
		// Vuepher models use singular struct names; let GORM pluralise them so
		// Post maps to `posts` without an explicit TableName method (DB-05,
		// generator naming rules).
		NamingStrategy: defaultNamingStrategy(),
		NowFunc: func() time.Time {
			return cfg.Now().UTC()
		},
	}

	var dialector gorm.Dialector
	driver := cfg.Driver
	switch driver {
	case "sqlite", "sqlite3", "":
		driver = "sqlite"
		dialector = sqlite.Open(cfg.DSN)
	case "postgres", "postgresql", "pgsql":
		driver = "postgres"
		dialector = postgres.Open(cfg.DSN)
	case "mysql", "mariadb":
		driver = "mysql"
		dialector = mysql.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf(
			"vuepher/orm: unsupported driver %q; set DB_CONNECTION to sqlite, postgres or mysql",
			cfg.Driver,
		)
	}

	gormDB, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("vuepher/orm: could not connect to %s: %w", driver, err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("vuepher/orm: could not reach the underlying sql.DB: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	sqlDB.SetConnMaxLifetime(time.Hour)

	db := &DB{DB: gormDB, driver: driver, logger: cfg.Logger, debug: cfg.Debug}
	return db, nil
}

// Driver returns the driver name in use ("sqlite", "postgres" or "mysql").
func (db *DB) Driver() string { return db.driver }

// Gorm returns the underlying *gorm.DB. This is the documented escape hatch
// (DB-10): there is no query the wrapper can express that GORM cannot.
func (db *DB) Gorm() *gorm.DB { return db.DB }

// Ping checks that the database is reachable, used by the health endpoint.
func (db *DB) Ping(ctx context.Context) error {
	if db == nil || db.DB == nil {
		return errors.New("vuepher/orm: no database connection")
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Close releases the connection pool (HTTP-11 graceful shutdown).
func (db *DB) Close() error {
	if db == nil || db.DB == nil {
		return nil
	}
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// WithContext returns a DB bound to a context, so queries are cancelled when the
// request is.
func (db *DB) WithContext(ctx context.Context) *DB {
	if db == nil || db.DB == nil {
		return db
	}
	return &DB{DB: db.DB.WithContext(ctx), driver: db.driver, logger: db.logger, debug: db.debug}
}

// Session returns a new session with the given options, for example
// gorm.Session{NewDB: true} to build a subquery.
func (db *DB) Session(opts *gorm.Session) *DB {
	if db == nil || db.DB == nil {
		return db
	}
	return &DB{DB: db.DB.Session(opts), driver: db.driver, logger: db.logger, debug: db.debug}
}

// Transaction runs fn inside a database transaction (DB-09). The callback
// receives a wrapped *DB so the wrapper's ergonomics stay available inside.
//
// Returning an error from fn rolls the transaction back and returns that error
// unchanged; a panic rolls back and re-panics.
//
//	err := app.DB.Transaction(ctx, func(tx *orm.DB) error {
//		if err := orm.Create(tx, &order); err != nil {
//			return err
//		}
//		return orm.Create(tx, &payment)
//	})
func (db *DB) Transaction(ctx context.Context, fn func(tx *DB) error) error {
	if db == nil || db.DB == nil {
		return errors.New("vuepher/orm: no database connection")
	}
	scope := db.DB.WithContext(ctx)
	return scope.Transaction(func(tx *gorm.DB) error {
		return fn(&DB{DB: tx, driver: db.driver, logger: db.logger, debug: db.debug})
	})
}

// raw returns the DB with no context, for internal helpers that already hold
// one.
func (db *DB) raw() *gorm.DB { return db.DB }

// ---------------------------------------------------------------------------
// Package-level connection
// ---------------------------------------------------------------------------

var (
	defaultMu   sync.RWMutex
	defaultConn *DB
)

// SetDefault installs the process-wide connection used by Query[T]() and the
// model helper methods. bootstrap calls it once after Connect.
func SetDefault(db *DB) {
	defaultMu.Lock()
	defaultConn = db
	defaultMu.Unlock()
}

// Default returns the process-wide connection, or nil when none is installed.
// A nil result is not a panic: terminal query methods turn it into a descriptive
// error instead.
func Default() *DB {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultConn
}

// ---------------------------------------------------------------------------
// Free functions mirroring the model helpers
// ---------------------------------------------------------------------------

// Create inserts a model using the given connection (DB-03).
func Create(db *DB, model any) error {
	if db == nil || db.DB == nil {
		return ErrNoConnection
	}
	return db.DB.Create(model).Error
}

// Save upserts a model: it inserts when the primary key is zero and updates
// otherwise.
func Save(db *DB, model any) error {
	if db == nil || db.DB == nil {
		return ErrNoConnection
	}
	return db.DB.Save(model).Error
}

// Delete removes a model, soft-deleting it when the model embeds
// vuepher.Model (DB-02).
func Delete(db *DB, model any) error {
	if db == nil || db.DB == nil {
		return ErrNoConnection
	}
	return db.DB.Delete(model).Error
}

// ErrNoConnection is returned by the wrapper when no database has been
// configured, so a missing bootstrap step produces a useful message instead of a
// nil dereference.
var ErrNoConnection = errors.New(
	"vuepher/orm: no database connection; call orm.Connect and orm.SetDefault during bootstrap",
)

// ErrNotFound is returned by First and Find when no row matches. It is distinct
// from a connection error so handlers can turn it into a 404.
var ErrNotFound = errors.New("vuepher/orm: record not found")

// ---------------------------------------------------------------------------
// GORM logger bridge
// ---------------------------------------------------------------------------

// newGormLogger maps Vuepher's log level onto GORM's logger, so database
// diagnostics go to the same place as everything else.
func newGormLogger(cfg Config) gormlogger.Interface {
	level := gormlogger.Warn
	switch cfg.LogLevel {
	case "silent":
		level = gormlogger.Silent
	case "error":
		level = gormlogger.Error
	case "warn", "":
		level = gormlogger.Warn
	case "info", "debug":
		level = gormlogger.Info
	}
	if cfg.Debug {
		level = gormlogger.Info
	}
	return gormlogger.Default.LogMode(level)
}
