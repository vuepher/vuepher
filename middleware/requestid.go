package middleware

import (
	"github.com/google/uuid"
	"github.com/vuepher/vuepher"
)

// RequestIDHeader is the header carrying the request identifier.
const RequestIDHeader = "X-Request-ID"

// requestIDKey is the context key the identifier is stored under.
const requestIDKey = "request_id"

// RequestID assigns every request an identifier and echoes it in the response
// (HTTP-05). An incoming header is honoured so a trace started upstream survives
// the hop.
//
// The identifier is also available to handlers as
// ctx.Get(RequestIDKey), which is what makes an error report referenceable from
// a log line.
func RequestID() vuepher.MiddlewareFunc {
	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		id := ctx.Header(RequestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		ctx.Set(requestIDKey, id)
		ctx.Response().Header().Set(RequestIDHeader, id)
		return next(ctx)
	}
}

// RequestIDKey is the context key under which RequestID stores the identifier.
// Exported so handlers and the logger can read it.
const RequestIDKey = requestIDKey

// RequestIDOf returns the request identifier, or "" when the middleware is not
// registered.
func RequestIDOf(ctx *vuepher.Context) string {
	value, ok := ctx.Get(RequestIDKey)
	if !ok {
		return ""
	}
	id, _ := value.(string)
	return id
}
