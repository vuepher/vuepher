package vuepher

import (
	"net/http"
	"net/url"
)

// Redirect describes a pending redirect. Handlers return it as an error so that
// middleware observes it like any other outcome (VIEW-07):
//
//	return ctx.Redirect("/posts").With("success", "Post created")
//
// Flash values survive exactly one subsequent request. They are read on the
// frontend through the shared `flash` prop injected by the ShareProps
// middleware (VIEW-05).
type Redirect struct {
	ctx      *Context
	url      string
	back     bool
	intended bool
	status   int
	flash    Props
	errors   ErrorBag
	errorBag string
	oldInput map[string]any
}

// Error makes *Redirect satisfy the error interface so handlers can return it.
// The message exists only for logging; a redirect is never rendered as an error
// page.
func (r *Redirect) Error() string {
	if r.back {
		return "vuepher: redirect back"
	}
	return "vuepher: redirect to " + r.url
}

// With flashes a value for the next request. The conventional keys are
// "success", "error" and "warning"; the frontend exposes them as
// page.props.flash.
func (r *Redirect) With(key string, value any) *Redirect {
	if r.flash == nil {
		r.flash = Props{}
	}
	r.flash[key] = value
	return r
}

// WithAll flashes several values at once.
func (r *Redirect) WithAll(values Props) *Redirect {
	for key, value := range values {
		r.With(key, value)
	}
	return r
}

// WithErrors flashes a validation error bag (VIEW-08). The errors are exposed to
// the next page as the `errors` prop, which is what useVuepherForm() reads.
func (r *Redirect) WithErrors(bag ErrorBag) *Redirect {
	r.errors = bag
	return r
}

// WithErrorsIn flashes a validation bag under a name, for pages that host more
// than one form (VIEW-08).
func (r *Redirect) WithErrorsIn(name string, bag ErrorBag) *Redirect {
	r.errors = bag
	r.errorBag = name
	return r
}

// WithInput flashes the submitted input so the next page can repopulate the
// form. Called automatically by WithErrors for the values that were just
// submitted.
func (r *Redirect) WithInput(inputs map[string]any) *Redirect {
	r.oldInput = inputs
	return r
}

// To overrides the redirect target.
func (r *Redirect) To(url string) *Redirect {
	r.url = url
	r.back = false
	return r
}

// Status overrides the redirect status code. Vuepher defaults to 302 for safe
// methods and 303 for PUT/PATCH/DELETE, so a redirect after a write becomes a
// GET (VIEW-01 step 6).
func (r *Redirect) Status(code int) *Redirect {
	r.status = code
	return r
}

// Intended records where the user was trying to go, so the login flow can send
// them back there afterwards (HTTP-09).
//
// It stores the *current* request URL, not the redirect target: the interesting
// value is the page the user was blocked from, not the login page they are being
// sent to. Retrieve it after login with ctx.Intended(fallback).
func (r *Redirect) Intended() *Redirect {
	r.intended = true
	return r
}

// URL returns the redirect target after resolution. Valid before perform() when
// the target is not "back".
func (r *Redirect) URL() string {
	if r.back {
		return r.ctx.resolveBackURL()
	}
	return r.url
}

// perform writes the redirect. It is invoked by the application's error handler,
// never by user code.
func (r *Redirect) perform() error {
	ctx := r.ctx
	target := r.URL()
	if target == "" {
		target = "/"
	}

	// Session-backed state: flash values, errors, old input and the intended
	// URL. Written before the response headers so the cookie travels with the
	// redirect.
	sess := ctx.Session()
	for key, value := range r.flash {
		sess.Flash(key, value)
	}
	if r.errors != nil {
		sess.FlashErrors(r.errors, r.errorBag)
		if r.oldInput == nil && ctx.HasAnyInput() {
			r.oldInput = ctx.Inputs()
		}
	}
	if r.oldInput != nil {
		sess.FlashInput(r.oldInput)
	}
	if r.intended {
		sess.Set(intendedSessionKey, ctx.URL())
	}
	// Persists the session cookie. Redirect responses are the common case for
	// flash data, so this must happen before WriteHeader.
	ctx.SaveSession()

	status := r.status
	if status == 0 {
		status = http.StatusFound
	}
	// A redirect that follows a write must become a GET, otherwise the browser
	// repeats the unsafe request.
	if ctx.Method() != http.MethodGet && ctx.Method() != http.MethodHead && status == http.StatusFound {
		status = http.StatusSeeOther
	}

	ctx.response.Header().Set("Location", target)
	ctx.response.WriteHeader(status)
	ctx.written = true
	return nil
}

// resolveBackURL decides where "back" points. The order is:
//
//  1. the referer header, when it belongs to this application;
//  2. the URL that was recorded as intended, if any;
//  3. "/"
func (c *Context) resolveBackURL() string {
	if ref := c.request.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil {
			// Same-origin only: a cross-origin referer would turn "back" into an
			// open redirect.
			if u.Host == "" || u.Host == c.request.Host {
				if target := u.RequestURI(); target != "" && target != "/" {
					return target
				}
			}
		}
	}
	if intended := c.Session().GetString(intendedSessionKey, ""); intended != "" {
		return intended
	}
	return "/"
}

// HasAnyInput reports whether the request carried a body worth preserving across
// a validation failure.
func (c *Context) HasAnyInput() bool {
	if c.request.Body == nil {
		return false
	}
	return c.request.Method == http.MethodPost ||
		c.request.Method == http.MethodPut ||
		c.request.Method == http.MethodPatch ||
		c.request.Method == http.MethodDelete
}
