package vuepher

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Router is the routing surface the framework exposes (HTTP-01). It is an
// interface with a Chi-backed default implementation so that the router can be
// swapped without touching application code (PRD §5.1). Fiber, for example,
// cannot be used directly because it is built on fasthttp and therefore
// incompatible with net/http middleware, http.FS for embedded assets, httptest
// and the OpenTelemetry ecosystem; if a throughput-driven adapter is ever added,
// it implements this interface.
type Router interface {
	// Use appends middleware applied to routes registered on this router and on
	// any group created from it afterwards. Register middleware before routes.
	Use(middleware ...MiddlewareFunc)

	// UseHTTP appends standard library middleware. It exists so that the large
	// net/http middleware ecosystem stays usable.
	UseHTTP(middleware ...HTTPMiddleware)

	// HTTP method registration. Each returns a Route handle for naming and
	// route-scoped middleware.
	Get(pattern string, handler HandlerFunc) Route
	Post(pattern string, handler HandlerFunc) Route
	Put(pattern string, handler HandlerFunc) Route
	Patch(pattern string, handler HandlerFunc) Route
	Delete(pattern string, handler HandlerFunc) Route
	Options(pattern string, handler HandlerFunc) Route
	Head(pattern string, handler HandlerFunc) Route

	// Any registers the pattern for every supported method.
	Any(pattern string, handler HandlerFunc) Route

	// Group runs fn with a router that inherits the current middleware stack.
	// Middleware added inside fn applies only to routes registered there.
	Group(fn func(Router))

	// Route nests a group under a path prefix and inherits middleware.
	Route(pattern string, fn func(Router))

	// Mount attaches a plain net/http handler, for file servers, health checks
	// and third-party handlers.
	Mount(pattern string, handler http.Handler)

	// Resource registers the CRUD routes for the actions a controller
	// implements (HTTP-02).
	Resource(name string, controller any)

	// URL builds the URL for a named route (HTTP-03).
	URL(name string, params ...any) string

	// Routes lists every registered route, for route:list and tests.
	Routes() []RouteInfo

	// Middleware returns the middleware applied to this router.
	Middleware() []MiddlewareFunc

	// Prefix is the path prefix this router is mounted under.
	Prefix() string

	// Chi returns the underlying chi.Router as an escape hatch.
	Chi() chi.Router

	http.Handler
}

// chiRouter implements Router on top of Chi.
//
// Middleware is tracked by Vuepher rather than handed to Chi's own Use, because
// Vuepher middleware needs the typed *Context rather than a raw http.Handler.
// Chi still owns matching, parameters and route patterns, which is the part
// worth delegating.
type chiRouter struct {
	app    *App
	mux    chi.Router
	group  []MiddlewareFunc
	prefix string
}

// newChiRouter builds the root router for an application.
func newChiRouter(app *App) *chiRouter {
	rt := &chiRouter{
		app: app,
		mux: chi.NewRouter(),
	}

	// Chi answers unmatched paths and methods itself, in plain text. Route both
	// through the application error path instead, so a 404 renders the
	// Errors/404 Vue page (VIEW-09) and an Inertia visit still receives a page
	// object rather than an HTML fragment it cannot mount.
	rt.mux.NotFound(rt.fallback(http.StatusNotFound, "Page not found."))
	rt.mux.MethodNotAllowed(rt.fallback(http.StatusMethodNotAllowed, "Method not allowed."))

	return rt
}

// fallback builds the handler Chi calls when no route matched. It runs the
// current global middleware chain, so sessions, shared props and the asset
// version behave exactly as they do for a registered route, then reports the
// status through the application error path.
//
// The middleware is read at request time for the same reason route entries read
// it at request time: middleware registered after boot must still apply.
func (rt *chiRouter) fallback(status int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := newContext(rt.app, w, r)

		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			for i, key := range routeCtx.URLParams.Keys {
				if i < len(routeCtx.URLParams.Values) {
					ctx.params[key] = routeCtx.URLParams.Values[i]
				}
			}
		}
		ctx.withContext()

		var err error = Abort(status, message)
		if runErr := chain(rt.Middleware(), func(*Context) error { return err })(ctx); runErr != nil {
			err = runErr
		}

		rt.app.handleError(ctx, err)
		rt.app.finishRequest(ctx)
	}
}

// child derives a router for a group. Middleware is copied so that additions
// inside the group do not leak to the parent.
func (rt *chiRouter) child(mux chi.Router, pattern string) *chiRouter {
	return &chiRouter{
		app:    rt.app,
		mux:    mux,
		group:  append([]MiddlewareFunc{}, rt.group...),
		prefix: rt.joinPath(pattern),
	}
}

func (rt *chiRouter) Use(middleware ...MiddlewareFunc) {
	rt.group = append(rt.group, middleware...)
}

func (rt *chiRouter) UseHTTP(middleware ...HTTPMiddleware) {
	for _, mw := range middleware {
		if mw == nil {
			continue
		}
		rt.mux.Use(mw)
	}
}

func (rt *chiRouter) Get(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodGet, pattern, handler)
}

func (rt *chiRouter) Post(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodPost, pattern, handler)
}

func (rt *chiRouter) Put(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodPut, pattern, handler)
}

func (rt *chiRouter) Patch(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodPatch, pattern, handler)
}

func (rt *chiRouter) Delete(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodDelete, pattern, handler)
}

func (rt *chiRouter) Options(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodOptions, pattern, handler)
}

func (rt *chiRouter) Head(pattern string, handler HandlerFunc) Route {
	return rt.handle(http.MethodHead, pattern, handler)
}

func (rt *chiRouter) Any(pattern string, handler HandlerFunc) Route {
	var first Route
	for _, method := range Methods {
		route := rt.handle(method, pattern, handler)
		if first == nil {
			first = route
		}
	}
	return first
}

func (rt *chiRouter) Group(fn func(Router)) {
	rt.mux.Group(func(sub chi.Router) {
		fn(rt.child(sub, ""))
	})
}

func (rt *chiRouter) Route(pattern string, fn func(Router)) {
	rt.mux.Route(pattern, func(sub chi.Router) {
		fn(rt.child(sub, pattern))
	})
}

func (rt *chiRouter) Mount(pattern string, handler http.Handler) {
	rt.mux.Mount(pattern, handler)
}

func (rt *chiRouter) Resource(name string, controller any) {
	base := "/" + strings.Trim(name, "/")
	singular := strings.TrimSuffix(strings.Trim(name, "/"), "s")
	if singular == "" {
		singular = strings.Trim(name, "/")
	}

	if c, ok := controller.(ResourceIndexer); ok {
		rt.Get(base, c.Index).Name(singular + ".index")
	}
	if c, ok := controller.(ResourceCreater); ok {
		rt.Get(base+"/create", c.Create).Name(singular + ".create")
	}
	if c, ok := controller.(ResourceStorer); ok {
		rt.Post(base, c.Store).Name(singular + ".store")
	}
	if c, ok := controller.(ResourceShower); ok {
		rt.Get(base+"/{id}", c.Show).Name(singular + ".show")
	}
	if c, ok := controller.(ResourceEditer); ok {
		rt.Get(base+"/{id}/edit", c.Edit).Name(singular + ".edit")
	}
	if c, ok := controller.(ResourceUpdater); ok {
		rt.Put(base+"/{id}", c.Update).Name(singular + ".update")
		rt.Patch(base+"/{id}", c.Update)
	}
	if c, ok := controller.(ResourceDestroyer); ok {
		rt.Delete(base+"/{id}", c.Destroy).Name(singular + ".destroy")
	}
}

func (rt *chiRouter) URL(name string, params ...any) string {
	if rt.app == nil || rt.app.routeRegistry == nil {
		return ""
	}
	return rt.app.routeRegistry.URL(name, params...)
}

func (rt *chiRouter) Routes() []RouteInfo {
	if rt.app == nil || rt.app.routeRegistry == nil {
		return nil
	}
	return rt.app.routeRegistry.List()
}

func (rt *chiRouter) Middleware() []MiddlewareFunc {
	return append([]MiddlewareFunc{}, rt.group...)
}

func (rt *chiRouter) Prefix() string { return rt.prefix }

func (rt *chiRouter) Chi() chi.Router { return rt.mux }

func (rt *chiRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mux.ServeHTTP(w, r)
}

// handle registers one route and returns its handle.
func (rt *chiRouter) handle(method, pattern string, handler HandlerFunc) Route {
	entry := &routeEntry{
		app:     rt.app,
		method:  method,
		path:    rt.joinPath(pattern),
		handler: handler,
		base:    append([]MiddlewareFunc{}, rt.group...),
	}
	rt.mux.Method(method, pattern, entry.httpHandler())
	if rt.app != nil && rt.app.routeRegistry != nil {
		rt.app.routeRegistry.register(entry)
	}
	return entry
}

// joinPath combines the router's prefix with a pattern for display in
// route:list. Matching itself is left to Chi, which knows the real mount points.
func (rt *chiRouter) joinPath(pattern string) string {
	prefix := strings.TrimSuffix(rt.prefix, "/")
	segment := pattern
	if segment != "" && !strings.HasPrefix(segment, "/") {
		segment = "/" + segment
	}
	joined := prefix + segment
	if joined == "" {
		return "/"
	}
	return joined
}

// httpHandler turns a route entry into an http.HandlerFunc. The entry is read at
// request time, so middleware attached after registration still runs.
func (e *routeEntry) httpHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := newContext(e.app, w, r)

		// Copy Chi's URL parameters into the context.
		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			for i, key := range routeCtx.URLParams.Keys {
				if i < len(routeCtx.URLParams.Values) {
					ctx.params[key] = routeCtx.URLParams.Values[i]
				}
			}
		}
		ctx.withContext()

		if err := chain(e.middleware(), e.handler)(ctx); err != nil {
			e.app.handleError(ctx, err)
		}
		e.app.finishRequest(ctx)
	}
}
