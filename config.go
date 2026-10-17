package vuepher

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config is the application configuration, loaded from a `.env` file and the
// process environment (HTTP-10). Values are exposed as typed structs so that
// mistakes surface at compile time rather than as a stringly-typed runtime
// lookup.
//
// Process environment variables win over `.env` entries, which is what you want
// in a container.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Session  SessionConfig
	Vite     ViteConfig

	// Paths that the CLI and the framework need at runtime.
	BasePath    string
	StoragePath string
	ViewsPath   string

	// warns collects non-fatal configuration problems found while loading, so
	// the caller can log them all at once instead of failing on the first.
	warns []string
}

// AppConfig is the [app] block.
type AppConfig struct {
	Name     string
	Env      string
	Debug    bool
	Key      string
	URL      string
	Timezone string
	Locale   string
}

// IsProduction reports whether the application runs in production, which
// enables secure cookies and suppresses internal error detail.
func (a AppConfig) IsProduction() bool { return a.Env == EnvProduction }

// IsLocal reports whether the application runs on a developer machine.
func (a AppConfig) IsLocal() bool { return a.Env == EnvLocal || a.Env == "development" }

// DatabaseConfig is the [database] block (DB-01).
type DatabaseConfig struct {
	Connection   string
	Database     string
	Host         string
	Port         int
	Username     string
	Password     string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
	LogLevel     string
}

// DSN builds the connection string for the configured driver. The sqlite DSN is
// a file path; the others are the conventional libpq/mysql URLs.
//
// TODO(phase-1): add an in-memory sqlite DSN for tests
// (`DB_CONNECTION=sqlite DB_DATABASE=:memory:`).
func (d DatabaseConfig) DSN() (string, error) {
	switch strings.ToLower(d.Connection) {
	case "sqlite", "sqlite3", "":
		if d.Database == "" {
			return "", errors.New("vuepher: DB_DATABASE must be set for the sqlite connection")
		}
		return d.Database, nil
	case "postgres", "postgresql", "pgsql":
		ssl := d.SSLMode
		if ssl == "" {
			ssl = "disable"
		}
		return fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			d.Host, d.Port, d.Username, d.Password, d.Database, ssl,
		), nil
	case "mysql", "mariadb":
		return fmt.Sprintf(
			"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			d.Username, d.Password, d.Host, d.Port, d.Database,
		), nil
	default:
		return "", fmt.Errorf("vuepher: unsupported DB_CONNECTION %q (expected sqlite, postgres or mysql)", d.Connection)
	}
}

// SessionConfig is the [session] block (HTTP-08).
type SessionConfig struct {
	Driver          string
	Name            string
	Path            string
	Domain          string
	LifetimeMinutes int
	Secure          bool
	SameSite        http.SameSite
}

// ViteConfig is the [vite] block (VIEW-03).
type ViteConfig struct {
	// DevServer is the Vite dev server origin, e.g. http://localhost:5173.
	DevServer string

	// UseDevServer is true while `vuepher dev` is running, which makes the
	// renderer emit Vite client tags instead of reading the build manifest.
	UseDevServer bool

	// BuildDir is where Vite writes production assets, relative to BasePath.
	BuildDir string

	// ManifestPath is the Vite manifest, relative to BasePath.
	ManifestPath string

	// HotFile is a marker file the CLI writes while the dev server is up, so a
	// Go process started outside `vuepher dev` can still detect it.
	HotFile string

	// EntryPoint is the Vite entry, e.g. resources/js/app.ts.
	EntryPoint string
}

// BasePath is the URL prefix built assets are served under: "/build/" for the
// default BuildDir of public/build.
//
// Vite has to be configured with the same value (vite.config.ts sets `base`).
// Without it Vite resolves the chunks it fetches at runtime relative to the site
// root, so every lazily-loaded page 404s: the entry script loads, the page
// component does not, and the browser shows an empty document.
func (v ViteConfig) BasePath() string {
	dir := v.BuildDir
	if dir == "" {
		dir = filepath.Join("public", "build")
	}
	return "/" + path.Base(filepath.ToSlash(dir)) + "/"
}

// LoadConfig reads configuration from envFile when it exists and overlays the
// process environment. A missing envFile is not an error.
func LoadConfig(envFile string) (*Config, error) {
	if envFile == "" {
		envFile = ".env"
	}
	if _, err := os.Stat(envFile); err == nil {
		// Existing environment variables take precedence, so Load is correct
		// here rather than Overload.
		if err := godotenv.Load(envFile); err != nil {
			return nil, fmt.Errorf("vuepher: could not read %s: %w", envFile, err)
		}
	}

	basePath, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		App: AppConfig{
			Name:     env("APP_NAME", "Vuepher"),
			Env:      env("APP_ENV", EnvLocal),
			Debug:    envBool("APP_DEBUG", true),
			Key:      env("APP_KEY", ""),
			URL:      env("APP_URL", "http://localhost:8080"),
			Timezone: env("APP_TIMEZONE", "UTC"),
			Locale:   env("APP_LOCALE", "en"),
		},
		Database: DatabaseConfig{
			Connection:   env("DB_CONNECTION", "sqlite"),
			Database:     env("DB_DATABASE", filepath.Join("storage", "database.sqlite")),
			Host:         env("DB_HOST", "127.0.0.1"),
			Port:         envInt("DB_PORT", 5432),
			Username:     env("DB_USERNAME", ""),
			Password:     env("DB_PASSWORD", ""),
			SSLMode:      env("DB_SSLMODE", "disable"),
			MaxOpenConns: envInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns: envInt("DB_MAX_IDLE_CONNS", 5),
			LogLevel:     env("DB_LOG_LEVEL", "warn"),
		},
		Session: SessionConfig{
			Driver:          env("SESSION_DRIVER", "cookie"),
			Name:            env("SESSION_NAME", "vuepher_session"),
			Path:            env("SESSION_PATH", "/"),
			Domain:          env("SESSION_DOMAIN", ""),
			LifetimeMinutes: envInt("SESSION_LIFETIME", 120),
			Secure:          envBool("SESSION_SECURE", false),
			SameSite:        parseSameSite(env("SESSION_SAME_SITE", "lax")),
		},
		Vite: ViteConfig{
			DevServer:    env("VITE_DEV_SERVER", "http://localhost:5173"),
			BuildDir:     "public/build",
			ManifestPath: "public/build/.vite/manifest.json",
			HotFile:      "public/hot",
			EntryPoint:   "resources/js/app.ts",
		},
		BasePath:    basePath,
		StoragePath: filepath.Join(basePath, "storage"),
		ViewsPath:   filepath.Join(basePath, "resources", "views"),
	}

	// The manifest location moved between Vite 4 and 5; accept either.
	if _, err := os.Stat(filepath.Join(basePath, cfg.Vite.ManifestPath)); err != nil {
		if _, legacyErr := os.Stat(filepath.Join(basePath, "public", "build", "manifest.json")); legacyErr == nil {
			cfg.Vite.ManifestPath = "public/build/manifest.json"
		}
	}
	if _, err := os.Stat(filepath.Join(basePath, cfg.Vite.HotFile)); err == nil {
		cfg.Vite.UseDevServer = true
	}

	if cfg.App.Key == "" {
		cfg.warn("APP_KEY is not set; sessions will use an insecure development key. Run `vuepher key:generate`.")
	}
	if cfg.App.IsProduction() && cfg.Session.Secure == false {
		cfg.warn("SESSION_SECURE is false in production; session cookies will be sent over plain HTTP.")
	}

	return cfg, nil
}

// Warnings returns the non-fatal problems found while loading configuration.
func (c *Config) Warnings() []string { return c.warns }

func (c *Config) warn(message string) { c.warns = append(c.warns, message) }

// Get reads a raw environment value with a fallback, for application-specific
// settings that do not belong in the framework's typed blocks.
func (c *Config) Get(key, def string) string { return env(key, def) }

// Timezone returns the configured location, falling back to UTC.
func (a AppConfig) Location() *time.Location {
	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ---------------------------------------------------------------------------
// Environment helpers
// ---------------------------------------------------------------------------

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func envBool(key string, def bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return b
}

func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(value) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
