# 0001. Chi as the HTTP router, behind a framework-owned interface

- **Status:** accepted
- **Date:** 2026-10-16
- **Deciders:** framework maintainers
- **Related:** PRD §5.1 (router decision), §5 (tech stack), §7.1 (performance),
  risk R7, HTTP-01 … HTTP-04

## Context

The PRD requires a router with path parameters, groups with prefixes, resource
routes, and a middleware pipeline — and it leaves the choice between Chi and
Fiber open until a benchmark is run (PRD §11, open question 1).

The forces that actually decide it:

1. **Ecosystem compatibility.** Fiber is built on `fasthttp`, which is not
   `net/http`. Anything written against `http.Handler`, `http.HandlerFunc`,
   `http.FS`, `httptest`, `httputil`, or OpenTelemetry/pprof instrumentation
   needs an adapter. `go:embed` + `http.FS` is the framework's asset story
   (VIEW-03, R8), and `httptest` is how the protocol contract tests are written
   (`tests/protocol_test.go`), so this is not a hypothetical.
2. **The performance target is modest where Fiber is strong.** NFR §7.1 asks for
   < 150 µs of framework overhead per request and ≥ 30 000 page-object requests
   per second without a database. Those are reachable on `net/http`; `fasthttp`
   wins on raw throughput and allocation count, which is not the bottleneck here.
3. **Debuggability.** A stack trace that goes through `net/http` is one a Go
   developer can read. Wrapping `fasthttp` in `net/http` compatibility shims
   (the usual `adaptor`) reintroduces the request/response copying that the raw
   performance was meant to buy.
4. **Reversibility.** Swapping the router later must not touch application code.

## Decision

Chi (`github.com/go-chi/chi/v5`) is the default router. It is used **only**
behind the framework's own `vuepher.Router` interface (`router.go`); no
application or framework package imports `chi` directly, apart from the adapter
itself.

The interface is deliberately small and already implemented by the adapter:

```go
type Router interface {
	Get(pattern string, handler HandlerFunc) Route
	Post / Put / Patch / Delete / Options / Head ...
	Group(fn func(Router))
	Route(pattern string, fn func(Router))
	Resource(name string, controller any)
	Mount(pattern string, handler http.Handler)
	Use(middleware ...MiddlewareFunc)
	UseHTTP(middleware ...HTTPMiddleware)
	URL(name string, params ...any) string
	Routes() []RouteInfo
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}
```

Two consequences of that choice are part of the decision, not incidental:

- **`NotFound` and `MethodNotAllowed` are framework handlers.** Chi answers
  unmatched paths in plain text, which would break VIEW-09 (a Vue error page) and
  would hand an SPA visit an HTML fragment it cannot mount. Both are routed
  through the application's error path, so a 404 is a page object like any other.
- **Middleware is `func(*vuepher.Context, HandlerFunc) error`**, with a
  `UseHTTP` escape hatch for standard `net/http` middleware. The typed form is
  what makes `ctx.View`, shared props and flash data pleasant to write; the escape
  hatch is what keeps the ecosystem reachable.

## Consequences

Easier:

- Every `net/http` middleware, instrumenter and test helper works as-is.
- `go:embed` assets are served with `http.FileServer`-style handlers and get
  `http.ServeContent` semantics (range requests, ETags) for free.
- A Fiber adapter is an additive package: implement `vuepher.Router`, no changes
  to the framework or to applications.

Harder, and accepted:

- Raw throughput is lower than a `fasthttp`-based router. If a future benchmark
  shows the framework CPU-bound inside the router, the answer is the Fiber
  adapter, not a rewrite.
- The interface hides Chi features applications might want (its `Context`
  values, `chi.Mux` sub-routers). `Chi()` on the adapter exposes the underlying
  router as the escape hatch, in the same spirit as the ORM wrapper.

Follow-up work created by this decision:

- A benchmark comparing Chi and Fiber behind the interface, on real handlers, to
  replace the estimate in §7.1 with a measurement.
- The Fiber adapter, which stays viable precisely because the interface exists.

## Alternatives considered

**Fiber.** Faster in isolation, and its API is familiar to Node developers. Rejected
because the ecosystem incompatibility is structural rather than a matter of
degrees: the cost lands on every middleware, every test and the asset pipeline,
while the benefit lands on a metric that is not the framework's bottleneck. The
`net/http` adaptor path reintroduces copies and hides the request type in stack
traces, which is the opposite of the DX goal.

**`net/http`'s own `ServeMux` (Go 1.22 patterns).** Attractive: zero
dependencies, and the new pattern syntax covers method + path parameters. Rejected
because groups-with-prefix, resource routes and per-route middleware — HTTP-02 and
HTTP-04 — would all have to be rebuilt, and rebuilding them is the part of a
router that grows.

**A custom router.** Rejected for the same reason the PRD rejects writing an ORM:
the problem is well solved, and the effort belongs in the integration (the view
bridge, the generators) that is the framework's actual contribution.
