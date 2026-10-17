package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/vuepher/vuepher"
)

// CSRFOptions configure CSRF protection.
type CSRFOptions struct {
	// TokenName is the form field and header holding the submitted token.
	// Defaults to "_token" for the field and "X-CSRF-Token" for the header.
	TokenName  string
	HeaderName string

	// SessionKey is where the session stores the expected token. Defaults to
	// the session's built-in token, so this is rarely needed.
	SessionKey string

	// Except lists route paths (or path prefixes ending in *) that skip the
	// check. Use it for webhook endpoints that authenticate by signature;
	// prefer adding a signature check to simply disabling CSRF.
	Except []string

	// ErrorHandler renders the rejection. Defaults to a 419-style response: the
	// framework returns 419 (Authentication Timeout) because that is the status
	// Laravel users expect and the frontend knows to reload on it.
	ErrorHandler func(*vuepher.Context, error) error
}

// StatusPageExpired is the status used for a failed CSRF check. It is not an
// official IANA code, but it is what the Laravel ecosystem uses, and keeping the
// number means frontend code can treat it identically.
const StatusPageExpired = 419

// CSRF rejects cross-site requests that carry a session cookie (HTTP-05, §7.4).
//
// The check covers unsafe methods only; GET, HEAD, OPTIONS and TRACE are
// expected to be side-effect free. The token is read from the request header
// first and the form body second, so both `useVuepherForm()` and a plain HTML
// form work without extra configuration.
//
// Register it after Session. It is enabled by default because the failure mode
// of forgetting it is a security hole, not a missing feature.
func CSRF(options ...CSRFOptions) vuepher.MiddlewareFunc {
	var opts CSRFOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if opts.TokenName == "" {
		opts.TokenName = "_token"
	}
	if opts.HeaderName == "" {
		opts.HeaderName = "X-CSRF-Token"
	}

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		if isSafeMethod(ctx.Method()) || matchesExcept(opts.Except, ctx.Path()) {
			return next(ctx)
		}

		session := ctx.Session()
		expected := session.Token()
		submitted := submittedToken(ctx, opts)

		if submitted == "" {
			return reject(ctx, opts, "The form has expired. Reload the page and try again.")
		}
		// Constant-time comparison: a timing side channel here would let an
		// attacker recover the token one byte at a time.
		if subtle.ConstantTimeCompare([]byte(expected), []byte(submitted)) != 1 {
			return reject(ctx, opts, "The form has expired. Reload the page and try again.")
		}
		return next(ctx)
	}
}

// reject renders the CSRF failure.
func reject(ctx *vuepher.Context, opts CSRFOptions, message string) error {
	err := vuepher.Abort(StatusPageExpired, message)
	if opts.ErrorHandler != nil {
		return opts.ErrorHandler(ctx, err)
	}
	// Let the framework's error handler render it, so the application gets its
	// own error page rather than a bare status code.
	return err
}

// submittedToken reads the token from the header, then the body, then the query
// string. The query string is last because a token in a URL leaks through
// referers and logs.
func submittedToken(ctx *vuepher.Context, opts CSRFOptions) string {
	if token := ctx.Header(opts.HeaderName); token != "" {
		return token
	}
	if token := ctx.Input(opts.TokenName); token != "" {
		return token
	}
	return ctx.Query(opts.TokenName)
}

// isSafeMethod reports whether a method is expected to be free of side effects.
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// matchesExcept reports whether a path is exempted. An entry ending in "*" is a
// prefix match.
func matchesExcept(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(pattern, "*")) {
				return true
			}
			continue
		}
		if pattern == path {
			return true
		}
	}
	return false
}
