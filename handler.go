package vuepher

import "net/http"

// HandlerFunc is a Vuepher request handler. Returning a non-nil error hands the
// error to the application's central error handler (HTTP-11): an *HTTPError is
// rendered with its status code, a *Redirect performs the redirect, anything
// else becomes a 500.
//
// Handlers return errors instead of writing them so that middleware can
// centralise logging, reporting and rendering.
type HandlerFunc func(*Context) error

// MiddlewareFunc is a Vuepher middleware. It receives the current context and a
// continuation for the rest of the pipeline. Middleware must call next exactly
// once, and should return next's error unchanged unless it is deliberately
// short-circuiting the request.
//
//	mw := func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
//		start := time.Now()
//		err := next(ctx)
//		log.Printf("%s %s took %s", ctx.Method(), ctx.Path(), time.Since(start))
//		return err
//	}
type MiddlewareFunc func(ctx *Context, next HandlerFunc) error

// HTTPMiddleware is a standard library middleware. Vuepher middleware is
// generally more convenient because it has typed access to *Context, but the
// ecosystem is full of net/http middleware (HTTP-04) and those must remain
// usable, so the router exposes UseHTTP for them.
type HTTPMiddleware func(http.Handler) http.Handler

// H adapts a handler function to a net/http handler. Useful for mounting plain
// net/http endpoints (health checks, metrics, file servers) on a Vuepher route
// without pulling the view bridge into them.
func H(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := FromRequest(r)
		if ctx == nil {
			http.Error(w, "vuepher: no request context; mount this handler on a Vuepher router", http.StatusInternalServerError)
			return
		}
		if err := fn(ctx); err != nil {
			ctx.app.handleError(ctx, err)
		}
	}
}

// chain composes middlewares around a final handler. Middlewares are applied in
// the order given: the first middleware is the outermost, so it sees the
// request first and the response last.
func chain(mws []MiddlewareFunc, final HandlerFunc) HandlerFunc {
	h := final
	for i := len(mws) - 1; i >= 0; i-- {
		mw := mws[i]
		next := h
		h = func(ctx *Context) error {
			return mw(ctx, next)
		}
	}
	return h
}
