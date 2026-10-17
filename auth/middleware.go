package auth

import (
	"net/http"

	"github.com/vuepher/vuepher"
)

// Middleware installs the guard on the context so handlers can call
// ctx.Auth() (HTTP-09). It never rejects a request; use Authenticate for that.
//
// Register it after Session and ShareProps, so the shared `auth` prop reflects
// the resolved user.
func Middleware(guard *SessionGuard) vuepher.MiddlewareFunc {
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		if guard == nil {
			return next(ctx)
		}
		ctx.SetAuth(guard.ForRequest(ctx))
		return next(ctx)
	}
}

// AuthenticateOptions configure the login redirect.
type AuthenticateOptions struct {
	// LoginRoute is the route name to redirect to. Defaults to "login".
	LoginRoute string

	// LoginPath is used when no route is registered under LoginRoute, so a
	// project without named routes still gets a sensible destination.
	LoginPath string

	// ForceFullReload makes an SPA visit answer 409 + X-Vuepher-Location
	// instead of a redirect the client would follow with an XHR. Use it when
	// the login page lives outside the Vue application.
	ForceFullReload bool
}

// Authenticate requires a signed-in user (HTTP-09). Unauthenticated requests are
// sent to the login page, and the page they were trying to reach is remembered
// so ctx.Intended() can send them back after signing in.
//
// For an SPA visit the answer depends on where login lives. When the login page
// is part of the Vue application, a plain redirect is correct and the client
// performs an SPA visit to it. When it is not — a server-rendered login page, or
// a different origin — ForceFullReload makes the framework reply with 409 and an
// X-Vuepher-Location header so the browser loads the page for real.
func Authenticate(guard *SessionGuard, options ...AuthenticateOptions) vuepher.MiddlewareFunc {
	var opts AuthenticateOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.LoginRoute == "" {
		opts.LoginRoute = "login"
	}
	if opts.LoginPath == "" {
		opts.LoginPath = "/login"
	}

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		requestGuard := guard.ForRequest(ctx)
		ctx.SetAuth(requestGuard)

		if requestGuard.Check() {
			return next(ctx)
		}

		target := loginURL(ctx, opts)
		if requestGuard.Err() != nil && ctx.App() != nil {
			ctx.App().Logger.Warn("auth: could not resolve the session user", "error", requestGuard.Err())
		}

		if opts.ForceFullReload {
			return ctx.Location(target)
		}
		return ctx.Redirect(target).Intended()
	}
}

// RedirectIfAuthenticated sends already-signed-in users away from the login and
// registration pages (HTTP-09).
func RedirectIfAuthenticated(guard *SessionGuard, to string) vuepher.MiddlewareFunc {
	if to == "" {
		to = "/"
	}
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		requestGuard := guard.ForRequest(ctx)
		ctx.SetAuth(requestGuard)
		if requestGuard.Check() {
			// Where they were heading before signing in takes precedence over
			// the dashboard, so "visit /posts, get bounced to login, sign in"
			// lands on /posts.
			return ctx.Redirect(ctx.Intended(to))
		}
		return next(ctx)
	}
}

// RequireAbility rejects the request unless the signed-in user satisfies the
// predicate (HTTP-09). It is the framework-free half of authorization; a richer
// policy system is planned for after v1.0.
//
//	router.Delete("/posts/{id}", c.Destroy).Middleware(
//		auth.RequireAbility(func(user any) bool {
//			u, ok := user.(*models.User)
//			return ok && u.IsAdmin
//		}),
//	)
func RequireAbility(predicate func(user any) bool) vuepher.MiddlewareFunc {
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		var user any
		if guard := ctx.Auth(); guard != nil {
			user = guard.User()
		}
		if user == nil {
			return vuepher.Abort(http.StatusUnauthorized, "You must sign in to continue.")
		}
		if predicate != nil && !predicate(user) {
			return vuepher.Forbidden("You are not allowed to do that.")
		}
		return next(ctx)
	}
}

// loginURL resolves the redirect target, preferring a named route.
func loginURL(ctx *vuepher.Context, opts AuthenticateOptions) string {
	if ctx.App() != nil {
		if url := ctx.App().URL(opts.LoginRoute); url != "" {
			return url
		}
	}
	return opts.LoginPath
}
