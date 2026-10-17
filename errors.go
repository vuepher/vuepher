package vuepher

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTPError carries an HTTP status code alongside an error message. Handlers
// and middleware return it to produce a specific status code; the central error
// handler renders it as a Vue error page for SPA/full-page requests, or as
// plain text for non-HTML clients.
type HTTPError struct {
	// Code is the HTTP status code to respond with.
	Code int

	// Message is the human-readable message shown to the user. It should not
	// leak internals in production; the error handler falls back to a generic
	// message when the application is not in debug mode.
	Message string

	// Headers are extra response headers to set when the error is rendered.
	Headers map[string]string

	// Err is the wrapped cause, reported to the error logger but never shown to
	// the user in production.
	Err error
}

func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("vuepher: %d %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("vuepher: %d %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped cause to errors.Is / errors.As.
func (e *HTTPError) Unwrap() error { return e.Err }

// WithHeader attaches a response header to the error and returns it for
// chaining.
func (e *HTTPError) WithHeader(key, value string) *HTTPError {
	if e.Headers == nil {
		e.Headers = make(map[string]string, 1)
	}
	e.Headers[key] = value
	return e
}

// Abort builds an HTTPError with the given status and message.
func Abort(code int, message string) *HTTPError {
	return &HTTPError{Code: code, Message: message}
}

// Abortf builds an HTTPError with a formatted message.
func Abortf(code int, format string, args ...any) *HTTPError {
	return &HTTPError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap attaches a cause to an HTTPError without losing the status code.
func Wrap(code int, message string, cause error) *HTTPError {
	return &HTTPError{Code: code, Message: message, Err: cause}
}

// Convenience constructors for the statuses applications reach for most.

func NotFound(message string) *HTTPError  { return Abort(http.StatusNotFound, message) }
func Forbidden(message string) *HTTPError { return Abort(http.StatusForbidden, message) }
func Unauthorized(message string) *HTTPError {
	return Abort(http.StatusUnauthorized, message)
}
func BadRequest(message string) *HTTPError { return Abort(http.StatusBadRequest, message) }
func Conflict(message string) *HTTPError   { return Abort(http.StatusConflict, message) }
func TooManyRequests(message string) *HTTPError {
	return Abort(http.StatusTooManyRequests, message)
}
func Internal(message string, cause error) *HTTPError {
	return Wrap(http.StatusInternalServerError, message, cause)
}

// StatusOf resolves the HTTP status code an error should be rendered with.
// Unknown errors become 500.
func StatusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var he *HTTPError
	if errors.As(err, &he) && he.Code != 0 {
		return he.Code
	}
	return http.StatusInternalServerError
}

// MessageOf resolves the message shown for an error.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}
	var he *HTTPError
	if errors.As(err, &he) {
		if he.Message != "" {
			return he.Message
		}
		return http.StatusText(he.Code)
	}
	return http.StatusText(http.StatusInternalServerError)
}

// IsRedirect reports whether err is a pending redirect. Redirects are returned
// as errors by handlers (VIEW-07) so that middleware sees them like any other
// outcome.
func IsRedirect(err error) bool {
	var r *Redirect
	return errors.As(err, &r)
}
