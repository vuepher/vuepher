// Package middleware holds Vuepher's built-in middleware (HTTP-05).
//
// They live in their own package rather than in the core so that an application
// only pays for what it registers; importing this package never pulls anything
// into the framework's own dependency graph.
//
// Typical registration order in bootstrap:
//
//	app.Use(
//		middleware.Recover(logger),   // outermost: must see every panic
//		middleware.RequestID(),
//		middleware.Logger(logger),
//		middleware.Session(app.Sessions),
//		middleware.ShareProps(),
//		middleware.CSRF(),
//	)
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/vuepher/vuepher"
)

// Recover converts a panic in a downstream handler into a 500 response and logs
// the stack trace (HTTP-05).
//
// Register it first, as the outermost middleware, or a panic in an earlier
// middleware will still take the process down. Go's net/http recovers per
// connection on its own, but without this middleware the client would see the
// connection close rather than an error page — and nothing would be logged.
//
// In development the panic value is included in the response so it is visible
// immediately; in production only a generic message is sent and the detail goes
// to the log.
func Recover(logger *slog.Logger) vuepher.MiddlewareFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) (err error) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// A broken pipe or a client disconnect is not an application bug and
			// must not be logged as one.
			if isBrokenPipe(recovered) {
				logger.Debug("vuepher: client disconnected", "path", ctx.Path())
				err = nil
				return
			}

			stack := debug.Stack()
			logger.Error(
				"vuepher: recovered from panic",
				"error", fmt.Sprint(recovered),
				"method", ctx.Method(),
				"path", ctx.Path(),
				"stack", string(stack),
			)

			if ctx.Written() {
				// Part of the response is already on the wire; all we can do is
				// abandon it.
				err = nil
				return
			}

			message := "Something went wrong."
			if ctx.App() != nil && ctx.App().Config.App.Debug {
				message = fmt.Sprintf("Panic: %v", recovered)
			}
			err = vuepher.Wrap(http.StatusInternalServerError, message, fmt.Errorf("panic: %v", recovered))
		}()
		return next(ctx)
	}
}

// isBrokenPipe reports whether a panic value is the error raised when the client
// goes away mid-response. A disconnected browser is normal and should not be
// reported as an application failure.
func isBrokenPipe(recovered any) bool {
	err, ok := recovered.(error)
	if !ok {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "client disconnected")
}
