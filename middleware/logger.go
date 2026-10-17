package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/vuepher/vuepher"
)

// LoggerOptions configure request logging.
type LoggerOptions struct {
	// Logger receives the entries. Defaults to slog.Default().
	Logger *slog.Logger

	// Skip receives the request path and reports whether to omit the entry.
	// Health checks and asset requests are the usual candidates.
	Skip func(path string) bool

	// SlowThreshold logs requests slower than this at warning level. The
	// framework's target is under 150 µs of overhead per request (PRD §7.1), so
	// anything above a few tens of milliseconds is worth pointing at.
	SlowThreshold time.Duration
}

// Logger records one entry per request with method, path, status and duration
// (HTTP-05).
//
// It always calls next, even when Skip matches, because it is a middleware and
// not a filter.
func Logger(options LoggerOptions) vuepher.MiddlewareFunc {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if options.SlowThreshold == 0 {
		options.SlowThreshold = 500 * time.Millisecond
	}

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		if options.Skip != nil && options.Skip(ctx.Path()) {
			return next(ctx)
		}

		started := time.Now()
		err := next(ctx)
		elapsed := time.Since(started)

		status := ctx.StatusCode()
		switch {
		case err != nil && vuepher.IsRedirect(err):
			status = http.StatusFound
		case err != nil:
			status = vuepher.StatusOf(err)
		}

		attrs := []any{
			"method", ctx.Method(),
			"path", ctx.URL(),
			"status", status,
			"duration", elapsed.String(),
		}
		if userID := userIDOf(ctx); userID != nil {
			attrs = append(attrs, "user", userID)
		}

		switch {
		case status >= 500:
			attrs = append(attrs, "error", errString(err))
			logger.Error("request failed", attrs...)
		case elapsed >= options.SlowThreshold:
			logger.Warn("slow request", attrs...)
		case status >= 400:
			logger.Info("request rejected", attrs...)
		default:
			logger.Info("request", attrs...)
		}
		return err
	}
}

// userIDOf reports the authenticated user's identifier when a guard is
// installed, so logs can be correlated per user without the application having
// to add it itself.
func userIDOf(ctx *vuepher.Context) any {
	guard := ctx.Auth()
	if guard == nil || !guard.Check() {
		return nil
	}
	return guard.ID()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
