package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/vuepher/vuepher"
)

// CORSOptions configure cross-origin resource sharing (HTTP-05).
type CORSOptions struct {
	// AllowedOrigins lists permitted origins. The single entry "*" allows any
	// origin, which cannot be combined with AllowCredentials — browsers reject
	// that combination and so does this middleware, loudly.
	AllowedOrigins []string

	// AllowedMethods defaults to the methods Vuepher routes support.
	AllowedMethods []string

	// AllowedHeaders defaults to Content-Type, X-Vuepher, X-CSRF-Token and
	// X-Requested-With — the headers the view bridge and CSRF layer use.
	AllowedHeaders []string

	// ExposedHeaders are response headers the browser may read. The view bridge
	// headers must be listed here for a cross-origin SPA to see a 409.
	ExposedHeaders []string

	// AllowCredentials permits cookies on cross-origin requests.
	AllowCredentials bool

	// MaxAge is how long a preflight result may be cached, in seconds.
	MaxAge int
}

// vuepherHeaders are the response headers the frontend adapter must be able to
// read, so they are exposed by default.
var vuepherHeaders = []string{
	vuepher.HeaderLocation,
	vuepher.HeaderVersion,
	vuepher.HeaderPartialData,
	vuepher.HeaderErrorBag,
}

// CORS answers preflight requests and adds the access-control headers (HTTP-05).
//
// Most Vuepher applications are same-origin and do not need this at all; it
// exists for a separately hosted frontend or a mobile client.
func CORS(options ...CORSOptions) vuepher.MiddlewareFunc {
	var opts CORSOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if len(opts.AllowedOrigins) == 0 {
		opts.AllowedOrigins = []string{"*"}
	}
	if len(opts.AllowedMethods) == 0 {
		opts.AllowedMethods = vuepher.Methods
	}
	if len(opts.AllowedHeaders) == 0 {
		opts.AllowedHeaders = []string{
			"Content-Type",
			"Accept",
			vuepher.HeaderRequest,
			vuepher.HeaderVersion,
			vuepher.HeaderPartialData,
			vuepher.HeaderPartialComponent,
			vuepher.HeaderPartialOnly,
			vuepher.HeaderPartialExcept,
			"X-CSRF-Token",
			"X-Requested-With",
		}
	}
	if len(opts.ExposedHeaders) == 0 {
		opts.ExposedHeaders = vuepherHeaders
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 86400
	}

	wildcard := false
	for _, origin := range opts.AllowedOrigins {
		if origin == "*" {
			wildcard = true
		}
	}
	invalidWildcardWithCredentials := wildcard && opts.AllowCredentials

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		origin := ctx.Header("Origin")
		if invalidWildcardWithCredentials {
			return vuepher.Internal(
				"vuepher: CORS is configured with a wildcard origin and credentials, "+
					"which browsers reject. List explicit origins in AllowedOrigins.",
				nil,
			)
		}

		if origin != "" && originAllowed(origin, opts.AllowedOrigins, wildcard) {
			header := ctx.Response().Header()
			// Echo the origin rather than sending "*" so credentials work.
			header.Set("Access-Control-Allow-Origin", origin)
			header.Add("Vary", "Origin")
			header.Set("Access-Control-Allow-Methods", strings.Join(opts.AllowedMethods, ", "))
			header.Set("Access-Control-Allow-Headers", strings.Join(opts.AllowedHeaders, ", "))
			header.Set("Access-Control-Expose-Headers", strings.Join(opts.ExposedHeaders, ", "))
			header.Set("Access-Control-Max-Age", strconv.Itoa(opts.MaxAge))
			if opts.AllowCredentials {
				header.Set("Access-Control-Allow-Credentials", "true")
			}
		}

		// Preflight requests are answered here and never reach a handler.
		// NoContent marks the response as written, so the framework does not add
		// its own "handler wrote nothing" error on top.
		if ctx.Method() == http.MethodOptions && ctx.Header("Access-Control-Request-Method") != "" {
			return ctx.NoContent()
		}
		return next(ctx)
	}
}

// originAllowed reports whether the request origin is permitted.
func originAllowed(origin string, allowed []string, wildcard bool) bool {
	if wildcard {
		return true
	}
	for _, candidate := range allowed {
		if candidate == origin {
			return true
		}
		// Support a leading wildcard for subdomains: *.example.com
		if strings.HasPrefix(candidate, "*.") {
			suffix := candidate[1:] // ".example.com"
			if strings.HasSuffix(origin, suffix) {
				return true
			}
			// Also match the bare domain.
			if strings.TrimPrefix(origin, "https://") == strings.TrimPrefix(suffix, ".") ||
				strings.TrimPrefix(origin, "http://") == strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}
