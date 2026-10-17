package middleware

import (
	"fmt"

	"github.com/vuepher/vuepher"
)

// Session loads the session for each request and installs it on the context
// (HTTP-08).
//
// Saving is not done here: a session cookie must be set before the response
// headers are written, so the framework persists the session from whichever code
// path writes the response. Doing it here after next() would be too late for
// anything that has already started writing.
//
// Register it before ShareProps, which reads flash data and the CSRF token out
// of the session.
func Session(store vuepher.SessionStore) vuepher.MiddlewareFunc {
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		if store == nil {
			return next(ctx)
		}
		session, err := store.Read(ctx.Request())
		if err != nil {
			// A store failure must not take the request down; the user simply
			// continues as a guest with an empty session.
			if ctx.App() != nil {
				ctx.App().Logger.Warn("vuepher: could not read the session", "error", err)
			}
			session = nil
		}
		if session != nil {
			ctx.SetSession(session)
		}
		return next(ctx)
	}
}

// StartSession is a convenience wrapper that also regenerates the session
// identifier, for routes where the session is being established rather than
// continued (login, checkout start). Regenerating on privilege change is what
// prevents session fixation (§7.4).
func StartSession(store vuepher.SessionStore) vuepher.MiddlewareFunc {
	base := Session(store)
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		return base(ctx, func(inner *vuepher.Context) error {
			inner.Session().Regenerate()
			return next(inner)
		})
	}
}

// RequireSession fails the request when no session is available, for routes that
// cannot work without one. Prefer Session in most cases: it degrades to a
// temporary session instead of erroring.
func RequireSession() vuepher.MiddlewareFunc {
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		if ctx.Session() == nil {
			return vuepher.Internal(
				fmt.Sprintf("vuepher: route %s requires a session but none is configured", ctx.Path()),
				nil,
			)
		}
		return next(ctx)
	}
}
