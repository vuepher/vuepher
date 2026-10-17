package vuepher

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/vuepher/vuepher/orm"
)

// App is the application: configuration, routing, database, sessions, rendering
// and the service container (HTTP-10, HTTP-12). A typical bootstrap is:
//
//	cfg, err := vuepher.LoadConfig(".env")
//	app := vuepher.NewApp(cfg)
//	app.Use(middleware.Recover(), middleware.Logger())
//	app.Use(middleware.Session(app.Sessions))
//	app.Use(middleware.ShareProps())
//	app.UseTemplates(os.DirFS("resources/views"), "app.html")
//	routes.Web(app)
//	app.Run(cfg.App.URL)
type App struct {
	// Config is the typed application configuration.
	Config *Config

	// Router is the HTTP router. Use the App's Get/Post/... shortcuts or this
	// field directly.
	Router Router

	// DB is the database handle (DB-01). It is nil until orm.Connect runs.
	DB *orm.DB

	// Container resolves application services (HTTP-12).
	Container *Container

	// Sessions stores session state (HTTP-08).
	Sessions SessionStore

	// Renderer turns components and props into responses (VIEW-01). Nil until
	// UseTemplates is called; rendering without it returns a 500 with an
	// explanatory message.
	Renderer *Renderer

	// Validator validates request structs (HTTP-07).
	Validator *Validator

	// Translator localises validation messages.
	Translator *Translator

	// Logger receives framework diagnostics.
	Logger *slog.Logger

	// Assets resolves the frontend build (VIEW-03). Mirrors Renderer.Assets()
	// for convenience.
	Assets AssetResolver

	// Resolver resolves container service names to values. It is the hook the
	// service container uses and the seam tests use to substitute fakes.
	Resolver func(name string) (any, error)

	routeRegistry *routeRegistry
	errorHandler  func(*Context, error) error

	mu       sync.RWMutex
	shutdown []func(context.Context) error
}

// NewApp builds an application from configuration. It does not touch the
// filesystem or the network; wiring is explicit so that tests can construct an
// App without a project layout on disk.
func NewApp(cfg *Config) *App {
	if cfg == nil {
		cfg = &Config{}
	}
	logLevel := slog.LevelInfo
	if cfg.App.Debug {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	validator := NewValidator(cfg.App.Locale)
	app := &App{
		Config:        cfg,
		Container:     NewContainer(),
		Validator:     validator,
		Translator:    validator.Translator,
		Logger:        logger,
		routeRegistry: newRouteRegistry(),
	}
	app.Sessions = newSessionStore(cfg)
	app.Router = newChiRouter(app)
	app.Resolver = app.Container.Make
	app.Container.Instance("app", app)
	app.Container.Instance("config", cfg)
	app.Container.Instance("logger", logger)
	app.Container.Instance("router", app.Router)
	app.Container.Instance("validator", validator)
	return app
}

// newSessionStore picks the store named by SESSION_DRIVER (HTTP-08).
func newSessionStore(cfg *Config) SessionStore {
	switch cfg.Session.Driver {
	case "memory", "array":
		return NewMemorySessionStore()
	case "database", "redis":
		// Fall back to the cookie driver and say so loudly rather than silently
		// losing sessions in production.
		slog.Default().Warn(
			"vuepher: session driver is not implemented yet, falling back to cookies",
			"driver", cfg.Session.Driver,
		)
		return NewCookieSessionStore(cfg.App.Key, cfg.Session)
	default:
		return NewCookieSessionStore(cfg.App.Key, cfg.Session)
	}
}

// ---------------------------------------------------------------------------
// Routing shortcuts
// ---------------------------------------------------------------------------

// Use appends global middleware. Call it before registering routes.
func (a *App) Use(middleware ...MiddlewareFunc) *App {
	a.Router.Use(middleware...)
	return a
}

// UseHTTP appends standard library middleware.
func (a *App) UseHTTP(middleware ...HTTPMiddleware) *App {
	a.Router.UseHTTP(middleware...)
	return a
}

// Get registers a GET route.
func (a *App) Get(pattern string, handler HandlerFunc) Route {
	return a.Router.Get(pattern, handler)
}

// Post registers a POST route.
func (a *App) Post(pattern string, handler HandlerFunc) Route {
	return a.Router.Post(pattern, handler)
}

// Put registers a PUT route.
func (a *App) Put(pattern string, handler HandlerFunc) Route {
	return a.Router.Put(pattern, handler)
}

// Patch registers a PATCH route.
func (a *App) Patch(pattern string, handler HandlerFunc) Route {
	return a.Router.Patch(pattern, handler)
}

// Delete registers a DELETE route.
func (a *App) Delete(pattern string, handler HandlerFunc) Route {
	return a.Router.Delete(pattern, handler)
}

// Group runs fn with a router inheriting the current middleware.
func (a *App) Group(fn func(Router)) { a.Router.Group(fn) }

// Route nests routes under a prefix.
func (a *App) Route(pattern string, fn func(Router)) { a.Router.Route(pattern, fn) }

// Resource registers CRUD routes for a controller.
func (a *App) Resource(name string, controller any) { a.Router.Resource(name, controller) }

// URL builds the URL for a named route.
func (a *App) URL(name string, params ...any) string { return a.Router.URL(name, params...) }

// RouteList returns every registered route, for route:list and for generating
// frontend route helpers (HTTP-03, FE-07).
func (a *App) RouteList() []RouteInfo { return a.Router.Routes() }

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// UseTemplates parses the application's root template and installs the default
// renderer (VIEW-02). fsys is normally os.DirFS("resources/views") in
// development and the embedded filesystem in production.
func (a *App) UseTemplates(fsys fs.FS, rootTemplate string) error {
	return a.UseRenderer(RendererOptions{FS: fsys, RootTemplate: rootTemplate})
}

// UseRenderer installs a renderer built from options, useful when the assets
// resolver or default title needs overriding.
func (a *App) UseRenderer(options RendererOptions) error {
	renderer, err := NewRenderer(a, options)
	if err != nil {
		return err
	}
	a.Renderer = renderer
	a.Assets = renderer.Assets()
	return nil
}

// SetRenderer installs a pre-built renderer, for applications that replace the
// default implementation.
func (a *App) SetRenderer(renderer *Renderer) {
	a.Renderer = renderer
	if renderer != nil {
		a.Assets = renderer.Assets()
	}
}

// ---------------------------------------------------------------------------
// Error handling and the request lifecycle
// ---------------------------------------------------------------------------

// OnError installs a custom error handler. Returning nil means the handler dealt
// with the error; returning the error falls back to the default rendering.
//
// The main uses are reporting to an external service and rendering a custom
// shape for JSON API routes.
func (a *App) OnError(fn func(*Context, error) error) *App {
	a.errorHandler = fn
	return a
}

// handleError renders an error response (HTTP-11). Redirects are executed here
// because handlers return them as errors.
func (a *App) handleError(ctx *Context, err error) {
	if err == nil {
		return
	}

	var redirect *Redirect
	if errors.As(err, &redirect) {
		if performErr := redirect.perform(); performErr != nil {
			a.reportError(performErr)
		}
		return
	}

	if a.errorHandler != nil {
		if handlerErr := a.errorHandler(ctx, err); handlerErr == nil {
			return
		}
	}

	a.reportError(err)

	if ctx.Written() {
		// The response is already partly sent; there is nothing safe to add.
		return
	}

	if a.Renderer != nil {
		if renderErr := a.Renderer.RenderError(ctx, err); renderErr != nil {
			a.reportError(renderErr)
			http.Error(ctx.response, MessageOf(err), StatusOf(err))
			ctx.written = true
		}
		return
	}

	http.Error(ctx.response, MessageOf(err), StatusOf(err))
	ctx.written = true
}

// reportError writes an error to the application log.
//
// Client errors are skipped: a missing page, an expired form and every other
// 4xx are ordinary traffic that the access log already records, and reporting
// each one at ERROR level buries the server faults that actually need
// attention. Server errors and non-HTTP errors are always reported.
func (a *App) reportError(err error) {
	if err == nil || a.Logger == nil {
		return
	}
	if StatusOf(err) < http.StatusInternalServerError {
		return
	}
	a.Logger.Error(err.Error())
}

// saveSession persists the session before the response headers are written.
// Cookies can only be set before WriteHeader, which is why this is called from
// every write path rather than once at the end of the request.
func (a *App) saveSession(ctx *Context, sess Session) error {
	if ctx.sessionSaved || sess == nil {
		return nil
	}
	if a.Sessions == nil {
		return nil
	}
	if err := a.Sessions.Write(ctx.response, sess); err != nil {
		return err
	}
	ctx.sessionSaved = true
	return nil
}

// finishRequest runs the end-of-request bookkeeping: it saves any pending
// session state and turns a handler that wrote nothing into a 500, because a
// silently empty response is almost always a bug.
func (a *App) finishRequest(ctx *Context) {
	if ctx.written {
		return
	}
	ctx.SaveSession()
	if !ctx.written {
		a.handleError(ctx, Internal("the handler returned without writing a response", nil))
	}
}

// OnShutdown registers a cleanup function run during graceful shutdown, in
// reverse registration order.
func (a *App) OnShutdown(fn func(context.Context) error) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.shutdown = append(a.shutdown, fn)
	return a
}

// ---------------------------------------------------------------------------
// Serving
// ---------------------------------------------------------------------------

// ServeHTTP makes App an http.Handler so it can be mounted in a larger server or
// exercised with httptest.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.Router.ServeHTTP(w, r)
}

// healthHandler serves the health check endpoint (HTTP-11).
func (a *App) healthHandler(ctx *Context) error {
	status := map[string]any{
		"status":  "ok",
		"version": Version,
	}
	if ctx.QueryBool("db", false) {
		if a.DB == nil {
			status["database"] = "not configured"
		} else if err := a.DB.Ping(ctx.Context()); err != nil {
			status["database"] = "unreachable"
			status["status"] = "degraded"
			ctx.Status(http.StatusServiceUnavailable)
		} else {
			status["database"] = "ok"
		}
	}
	return ctx.JSON(status)
}

// RegisterHealth registers the health check route (HTTP-11).
func (a *App) RegisterHealth(path string) *App {
	if path == "" {
		path = "/up"
	}
	a.Get(path, a.healthHandler)
	return a
}

// Run starts the HTTP server and blocks until a termination signal arrives, then
// shuts down gracefully (HTTP-11).
func (a *App) Run(addr string) error {
	if addr == "" {
		addr = a.Config.App.URL
	}
	addr = normaliseAddr(addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           a,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info("vuepher: server listening", "addr", addr, "env", a.Config.App.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("vuepher: server failed: %w", err)
	case <-signals:
		a.Logger.Info("vuepher: shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.Shutdown(shutdownCtx, server)
}

// Shutdown stops the server and runs registered cleanup functions.
func (a *App) Shutdown(ctx context.Context, server *http.Server) error {
	var firstErr error
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}

	a.mu.RLock()
	hooks := make([]func(context.Context) error, len(a.shutdown))
	copy(hooks, a.shutdown)
	a.mu.RUnlock()

	for i := len(hooks) - 1; i >= 0; i-- {
		if err := hooks[i](ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if a.DB != nil {
		if err := a.DB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// normaliseAddr accepts either a full URL (APP_URL=http://localhost:8080) or a
// bare address (:8080) and returns something http.Server understands.
func normaliseAddr(addr string) string {
	if addr == "" {
		return ":8080"
	}
	if idx := indexOf(addr, "://"); idx >= 0 {
		rest := addr[idx+3:]
		if slash := indexOf(rest, "/"); slash >= 0 {
			rest = rest[:slash]
		}
		addr = rest
	}
	if addr[0] == ':' {
		return addr
	}
	// A bare host without a port cannot be listened on.
	if indexOf(addr, ":") < 0 {
		return addr + ":8080"
	}
	return addr
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
