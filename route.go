package vuepher

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Resource controllers (HTTP-02)
// ---------------------------------------------------------------------------

// A resource controller implements one interface per action it supports.
// router.Resource registers only the actions that are present, so a read-only
// controller is a first-class thing rather than a controller full of methods
// returning 405:
//
//	type PostController struct{}
//
//	func (c *PostController) Index(ctx *vuepher.Context) error { ... }
//	func (c *PostController) Show(ctx *vuepher.Context) error  { ... }
//
//	router.Resource("posts", &PostController{})
//
// `vuepher make:controller PostController --resource` generates all seven.
type (
	// ResourceIndexer handles GET /posts.
	ResourceIndexer interface{ Index(*Context) error }

	// ResourceCreater handles GET /posts/create.
	ResourceCreater interface{ Create(*Context) error }

	// ResourceStorer handles POST /posts.
	ResourceStorer interface{ Store(*Context) error }

	// ResourceShower handles GET /posts/{id}.
	ResourceShower interface{ Show(*Context) error }

	// ResourceEditer handles GET /posts/{id}/edit.
	ResourceEditer interface{ Edit(*Context) error }

	// ResourceUpdater handles PUT and PATCH /posts/{id}.
	ResourceUpdater interface{ Update(*Context) error }

	// ResourceDestroyer handles DELETE /posts/{id}.
	ResourceDestroyer interface{ Destroy(*Context) error }
)

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// Route is the handle returned when a route is registered. It exists so that
// naming and route-scoped middleware read as a chain:
//
//	router.Get("/posts/{id}", c.Show).Name("posts.show").Middleware(auth.Require)
type Route interface {
	// Name registers a name usable with URL generation.
	Name(name string) Route

	// Middleware appends middleware that runs after group middleware and before
	// the handler.
	Middleware(middleware ...MiddlewareFunc) Route

	// Info describes the route, for route:list and tests.
	Info() RouteInfo
}

// RouteInfo is the public, serialisable description of a registered route.
type RouteInfo struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	Name         string `json:"name,omitempty"`
	Middleware   int    `json:"middleware"`
	ResourceName string `json:"resource,omitempty"`
}

// routeEntry is the concrete Route. The HTTP handler registered with the router
// closes over the entry, so middleware and the name added after registration are
// visible when the request arrives.
type routeEntry struct {
	app      *App
	method   string
	path     string
	name     string
	handler  HandlerFunc
	base     []MiddlewareFunc
	extra    []MiddlewareFunc
	mu       sync.RWMutex
	resource string
}

// Name registers a name for URL generation.
func (e *routeEntry) Name(name string) Route {
	e.mu.Lock()
	e.name = name
	e.mu.Unlock()
	if e.app != nil {
		if err := e.app.routeRegistry.name(name, e); err != nil {
			e.app.reportError(err)
		}
	}
	return e
}

// Middleware appends route-scoped middleware.
func (e *routeEntry) Middleware(middleware ...MiddlewareFunc) Route {
	e.mu.Lock()
	e.extra = append(e.extra, middleware...)
	e.mu.Unlock()
	return e
}

// Info describes the route.
func (e *routeEntry) Info() RouteInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return RouteInfo{
		Method:       e.method,
		Path:         e.path,
		Name:         e.name,
		Middleware:   len(e.base) + len(e.extra),
		ResourceName: e.resource,
	}
}

// middleware returns the middleware chain for this route: the middleware active
// when the route was registered plus anything added with Middleware().
func (e *routeEntry) middleware() []MiddlewareFunc {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]MiddlewareFunc, 0, len(e.base)+len(e.extra))
	out = append(out, e.base...)
	out = append(out, e.extra...)
	return out
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// routeRegistry tracks every registered route and resolves named routes to URLs
// (HTTP-03). It also feeds `vuepher route:list` and frontend route helpers.
type routeRegistry struct {
	mu    sync.RWMutex
	all   []*routeEntry
	named map[string]*routeEntry
}

func newRouteRegistry() *routeRegistry {
	return &routeRegistry{named: map[string]*routeEntry{}}
}

// register adds a route to the registry.
func (r *routeRegistry) register(entry *routeEntry) {
	r.mu.Lock()
	r.all = append(r.all, entry)
	r.mu.Unlock()
}

// name binds a name to a route. Naming two routes the same is a programming
// error and is reported rather than silently overwritten.
func (r *routeRegistry) name(name string, entry *routeEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, taken := r.named[name]; taken && existing != entry {
		return fmt.Errorf(
			"vuepher: route name %q is already used by %s %s; route names must be unique",
			name, existing.method, existing.path,
		)
	}
	r.named[name] = entry
	return nil
}

// lookup finds a named route.
func (r *routeRegistry) lookup(name string) (*routeEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.named[name]
	return entry, ok
}

// URL builds the URL for a named route (HTTP-03). Parameters can be passed
// positionally:
//
//	route.URL("posts.show", 42)              // /posts/42
//
// or by name, which lets callers skip optional segments:
//
//	route.URL("posts.comments.show", map[string]any{"post": 42, "comment": 7})
//
// An unknown name returns "" rather than panicking, so a typo in a redirect
// produces a redirect to "/" instead of taking the server down. In debug mode it
// is reported to the error log.
func (r *routeRegistry) URL(name string, params ...any) string {
	entry, ok := r.lookup(name)
	if !ok {
		return ""
	}
	pattern := entry.path
	if len(params) == 0 {
		return pattern
	}

	switch named := params[0].(type) {
	case map[string]any:
		for key, value := range named {
			pattern = strings.ReplaceAll(pattern, "{"+key+"}", escapeParam(fmt.Sprint(value)))
		}
		return pattern
	case map[string]string:
		for key, value := range named {
			pattern = strings.ReplaceAll(pattern, "{"+key+"}", escapeParam(value))
		}
		return pattern
	}

	return fillPositional(pattern, params)
}

// fillPositional substitutes placeholders in the order they appear.
func fillPositional(pattern string, params []any) string {
	remaining := pattern
	for i, param := range params {
		start := strings.Index(remaining, "{")
		if start < 0 {
			break
		}
		end := strings.Index(remaining[start:], "}")
		if end < 0 {
			break
		}
		end += start
		if i >= len(params) {
			break
		}
		remaining = remaining[:start] + escapeParam(fmt.Sprint(param)) + remaining[end+1:]
	}
	// Strip any segments whose parameters were not supplied, so that a route
	// like /posts/{id}/edit degrades to /posts when the id is missing rather
	// than emitting a literal placeholder.
	if idx := strings.Index(remaining, "{"); idx >= 0 {
		remaining = strings.TrimSuffix(remaining[:idx], "/")
	}
	return remaining
}

// escapeParam percent-escapes a path parameter. Slashes become %2F so a
// parameter cannot inject extra path segments.
func escapeParam(value string) string {
	replacer := strings.NewReplacer("/", "%2F", "?", "%3F", "#", "%23", " ", "%20")
	return replacer.Replace(value)
}

// List returns every registered route, sorted by path then method, for
// `vuepher route:list`.
func (r *routeRegistry) List() []RouteInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RouteInfo, 0, len(r.all))
	for _, entry := range r.all {
		out = append(out, entry.Info())
	}
	// Stable, readable ordering: path first, then method, then name.
	sortRouteInfos(out)
	return out
}

// Count reports how many routes are registered.
func (r *routeRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.all)
}

func sortRouteInfos(items []RouteInfo) {
	// Insertion sort keeps this dependency-free and the lists are tiny.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			less := items[j].Path < items[j-1].Path ||
				(items[j].Path == items[j-1].Path && items[j].Method < items[j-1].Method)
			if !less {
				break
			}
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Method helpers
// ---------------------------------------------------------------------------

// Methods lists the HTTP methods Vuepher routes support.
var Methods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
	http.MethodHead,
}
