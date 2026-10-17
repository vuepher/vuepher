package vuepher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// contextKey is the unexported key type used to stash the Vuepher Context on the
// standard library request context, so that plain net/http handlers and
// net/http middleware can reach it without the router leaking the pointer.
type contextKey struct{}

var ctxKey = contextKey{}

// Context wraps the request and response for a single HTTP request (HTTP-06). It
// is the framework's primary API surface: input access, validation, sessions,
// authentication, rendering and redirects all hang off it.
//
// A Context is created by the router for each request and must not be retained
// beyond it.
type Context struct {
	app      *App
	request  *http.Request
	response http.ResponseWriter

	params    map[string]string
	loadedIdx int // number of response bytes written, for logging

	// Lazy request body parsing. Both are guarded by once values so that
	// handlers can call Input/Bind freely without paying for parsing twice.
	bodyOnce sync.Once
	body     []byte
	bodyErr  error

	formOnce sync.Once
	form     url.Values
	formErr  error

	jsonOnce sync.Once
	jsonBody map[string]any
	jsonErr  error

	status       int
	written      bool
	sessionSaved bool
	session      Session
	auth         Auth
	errors       ErrorBag
	shared       Props
	redirect     *Redirect

	// locals are request-scoped values set by middleware and middleware-ish
	// helpers (ctx.Set / ctx.Get). Not exposed to the page object.
	locals map[string]any
}

// newContext builds a Context. Called by the router.
func newContext(app *App, w http.ResponseWriter, r *http.Request) *Context {
	return &Context{
		app:      app,
		request:  r,
		response: w,
		params:   map[string]string{},
		status:   http.StatusOK,
		errors:   ErrorBag{},
		shared:   Props{},
		locals:   map[string]any{},
	}
}

// Request returns the underlying *http.Request. Escape hatch for middleware and
// third-party net/http integrations.
func (c *Context) Request() *http.Request { return c.request }

// Response returns the underlying http.ResponseWriter. Prefer the typed helpers
// on Context; this exists for streaming and hijacking.
func (c *Context) Response() http.ResponseWriter { return c.response }

// App returns the application this request belongs to.
func (c *Context) App() *App { return c.app }

// Context returns the request's standard library context, suitable for
// cancellation-aware database calls.
func (c *Context) Context() context.Context { return c.request.Context() }

// ---------------------------------------------------------------------------
// Request metadata
// ---------------------------------------------------------------------------

// Method returns the HTTP method.
func (c *Context) Method() string { return c.request.Method }

// Path returns the request path without the query string.
func (c *Context) Path() string { return c.request.URL.Path }

// URL returns the full request URL including the query string, which is what the
// page object reports to the client.
func (c *Context) URL() string {
	if c.request.URL.RawQuery != "" {
		return c.request.URL.Path + "?" + c.request.URL.RawQuery
	}
	return c.request.URL.Path
}

// IsPageRequest reports whether the client expects a JSON page object rather
// than a full HTML document.
func (c *Context) IsPageRequest() bool { return c.pageRequest().IsPageRequest }

// pageRequest parses the view-bridge headers for this request.
func (c *Context) pageRequest() PageRequest { return parsePageRequest(c.request) }

// Header returns a request header.
func (c *Context) Header(key string) string { return c.request.Header.Get(key) }

// Param returns a route parameter, e.g. the {id} in /posts/{id}.
func (c *Context) Param(key string) string { return c.params[key] }

// Params returns a copy of all route parameters.
func (c *Context) Params() map[string]string {
	out := make(map[string]string, len(c.params))
	for k, v := range c.params {
		out[k] = v
	}
	return out
}

// ParamInt parses a route parameter as an integer. Returns def when the
// parameter is absent or not a number; handlers that require a valid numeric
// parameter should check with strconv themselves and return a 404.
func (c *Context) ParamInt(key string, def int) int {
	v := c.params[key]
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Set stores a request-scoped value. Retrieve it with Get. Values set here are
// never serialised into the page object.
func (c *Context) Set(key string, value any) { c.locals[key] = value }

// Get retrieves a value stored with Set.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.locals[key]
	return v, ok
}

// ---------------------------------------------------------------------------
// Input (HTTP-06)
// ---------------------------------------------------------------------------

// Query returns a query-string value, or def when absent.
func (c *Context) Query(key string, def ...string) string {
	if v := c.request.URL.Query().Get(key); v != "" {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}
	return ""
}

// QueryInt returns a query-string value as an integer, or def when absent or
// unparseable. Used by the paginator.
func (c *Context) QueryInt(key string, def int) int {
	raw := c.request.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

// QueryBool returns a query-string value as a boolean.
func (c *Context) QueryBool(key string, def bool) bool {
	raw := c.request.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return b
}

// parseForm parses the URL query plus, for form-encoded requests, the body.
// Called by Input/Bind. It never returns an error to the caller: a malformed
// body is surfaced through Bind.
func (c *Context) parseForm() {
	c.formOnce.Do(func() {
		if c.request.Method == http.MethodGet || c.request.Method == http.MethodHead {
			return
		}
		ct := c.request.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			c.formErr = c.request.ParseMultipartForm(32 << 20)
			if c.request.MultipartForm != nil {
				c.form = url.Values{}
				for k, v := range c.request.MultipartForm.Value {
					c.form[k] = v
				}
				return
			}
			return
		}
		c.formErr = c.request.ParseForm()
		c.form = c.request.PostForm
	})
}

// maxBodyBytes caps how much of a request body is buffered in memory. The limit
// matches the multipart limit used by parseForm; a body larger than this is
// rejected rather than truncated, because a silently shortened body would fail
// later as a confusing decode error.
const maxBodyBytes = 32 << 20 // 32 MiB

// rawBody returns the request body, reading and caching it on first use.
//
// The body is buffered rather than consumed in place because more than one part
// of a request reads it: the CSRF middleware reads a token out of a form post, a
// controller binds it into a request struct, and the redirect that follows a
// validation failure flashes the submitted values back as old input. Reading it
// directly would leave whoever runs second with an empty body, which is how old
// input silently goes missing. The bytes are put back on the request as well, so
// a third-party handler or net/http middleware downstream still sees them.
func (c *Context) rawBody() []byte {
	c.bodyOnce.Do(func() {
		if c.request.Body == nil {
			return
		}
		// Read one byte past the limit so an oversized body can be told apart
		// from one that is exactly at it.
		body, err := io.ReadAll(io.LimitReader(c.request.Body, maxBodyBytes+1))
		if err != nil {
			c.bodyErr = Wrap(http.StatusBadRequest, "the request body could not be read", err)
			return
		}
		if len(body) > maxBodyBytes {
			c.bodyErr = Abort(http.StatusRequestEntityTooLarge, "the request body is too large")
			return
		}
		c.body = body
		c.request.Body = io.NopCloser(bytes.NewReader(body))
	})
	return c.body
}

// RawBody returns the buffered request body. The body is read once and cached,
// so calling it after Input or Bind returns the same bytes rather than nothing.
func (c *Context) RawBody() ([]byte, error) {
	body := c.rawBody()
	if c.bodyErr != nil {
		return nil, c.bodyErr
	}
	return body, nil
}

// parseJSONBody decodes a JSON request body into a generic map. Called by
// Input/Bind for application/json requests.
func (c *Context) parseJSONBody() {
	c.jsonOnce.Do(func() {
		if !strings.HasPrefix(c.request.Header.Get("Content-Type"), "application/json") {
			return
		}
		body, err := c.RawBody()
		if err != nil {
			c.jsonErr = err
			return
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(&c.jsonBody); err != nil {
			if !errors.Is(err, io.EOF) {
				c.jsonErr = err
			}
			return
		}
	})
}

// Input returns a request input value, looking first at the JSON body (for
// application/json requests) and then at form/query values. Returns def when the
// key is absent.
func (c *Context) Input(key string, def ...string) string {
	fallback := ""
	if len(def) > 0 {
		fallback = def[0]
	}
	if v, ok := c.inputRaw(key); ok {
		return toString(v)
	}
	if v := c.Query(key); v != "" {
		return v
	}
	return fallback
}

// InputInt returns an input value coerced to an integer.
func (c *Context) InputInt(key string, def int) int {
	if v, ok := c.inputRaw(key); ok {
		if n, err := strconv.Atoi(toString(v)); err == nil {
			return n
		}
	}
	return c.QueryInt(key, def)
}

// InputBool returns an input value coerced to a boolean.
func (c *Context) InputBool(key string, def bool) bool {
	if v, ok := c.inputRaw(key); ok {
		if b, err := strconv.ParseBool(toString(v)); err == nil {
			return b
		}
	}
	return c.QueryBool(key, def)
}

// Inputs returns all inputs as a flat map. Nested JSON values are returned as
// they were decoded.
func (c *Context) Inputs() map[string]any {
	out := map[string]any{}
	c.parseForm()
	for k, values := range c.form {
		if len(values) == 1 {
			out[k] = values[0]
			continue
		}
		out[k] = values
	}
	c.parseJSONBody()
	for k, v := range c.jsonBody {
		out[k] = v
	}
	return out
}

// HasInput reports whether a key is present in the request body (not the query
// string). Distinguishing present-but-empty from absent matters for PATCH-style
// updates.
func (c *Context) HasInput(key string) bool {
	_, ok := c.inputRaw(key)
	return ok
}

func (c *Context) inputRaw(key string) (any, bool) {
	c.parseJSONBody()
	if c.jsonBody != nil {
		if v, ok := c.jsonBody[key]; ok {
			return v, true
		}
	}
	c.parseForm()
	if c.form != nil {
		if values, ok := c.form[key]; ok && len(values) > 0 {
			return values[0], true
		}
	}
	return nil, false
}

// Bind decodes the request payload into dst (HTTP-06, HTTP-07).
//
// JSON requests decode directly. Form and multipart requests are normalised
// through a map so that `json` struct tags work uniformly, which is what makes a
// request struct usable for both a JSON API call and an HTML form post.
//
// TODO(phase-1): support `form` tags and slice-of-struct binding for
// `items[0].title` style inputs.
func (c *Context) Bind(dst any) error {
	if strings.HasPrefix(c.request.Header.Get("Content-Type"), "application/json") {
		body, err := c.RawBody()
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return BadRequest("request body is empty")
		}
		// Unknown keys are ignored rather than rejected, which is what makes a
		// request struct interchangeable between a JSON call and a form post:
		// the form path has always ignored them, and a client that sends one
		// extra field should get a validation error it can act on, not an
		// opaque decode failure. What matters is covered by the validate tags.
		if err := json.Unmarshal(body, dst); err != nil {
			return Wrap(http.StatusUnprocessableEntity, "the request payload could not be decoded", err)
		}
		return nil
	}

	c.parseForm()
	if c.formErr != nil {
		return Wrap(http.StatusBadRequest, "the request body could not be parsed", c.formErr)
	}
	payload := map[string]any{}
	for k, values := range c.form {
		if len(values) == 1 {
			payload[k] = values[0]
			continue
		}
		payload[k] = values
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return Wrap(http.StatusBadRequest, "the request body could not be decoded", err)
	}
	if err := json.Unmarshal(buf, dst); err != nil {
		return Wrap(http.StatusUnprocessableEntity, "the request payload does not match the expected shape", err)
	}
	return nil
}

// File returns an uploaded file by field name (HTTP-06).
func (c *Context) File(name string) (*multipart.FileHeader, error) {
	c.parseForm()
	if c.request.MultipartForm == nil {
		return nil, Abortf(http.StatusBadRequest, "no file was uploaded as %q", name)
	}
	files := c.request.MultipartForm.File[name]
	if len(files) == 0 {
		return nil, Abortf(http.StatusBadRequest, "no file was uploaded as %q", name)
	}
	return files[0], nil
}

// Cookie returns a request cookie, or an error when it is absent.
func (c *Context) Cookie(name string) (*http.Cookie, error) {
	return c.request.Cookie(name)
}

// SetCookie writes a response cookie with secure defaults (HttpOnly,
// SameSite=Lax, and Secure outside local development).
func (c *Context) SetCookie(cookie *http.Cookie) {
	if cookie.Path == "" {
		cookie.Path = "/"
	}
	if !cookie.HttpOnly {
		cookie.HttpOnly = true
	}
	if cookie.SameSite == http.SameSiteDefaultMode {
		cookie.SameSite = http.SameSiteLaxMode
	}
	if c.app != nil && !c.app.Config.Session.Secure && c.app.Config.App.Env == EnvProduction {
		cookie.Secure = true
	}
	http.SetCookie(c.response, cookie)
}

// DeleteCookie expires a response cookie (DB-adjacent helper for logout flows).
func (c *Context) DeleteCookie(name string) {
	http.SetCookie(c.response, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ---------------------------------------------------------------------------
// Request-scoped services
// ---------------------------------------------------------------------------

// Session returns the session for this request. Never nil: the session
// middleware installs one, and a memory-backed session is created lazily if the
// middleware is absent (so unit tests can call handlers directly).
func (c *Context) Session() Session {
	if c.session == nil {
		if c.app != nil && c.app.Sessions != nil {
			s, err := c.app.Sessions.Read(c.request)
			if err != nil {
				c.app.reportError(err)
			}
			c.session = s
		}
		if c.session == nil {
			c.session = newMemorySession()
		}
	}
	return c.session
}

// SetSession installs a session. Called by the session middleware.
func (c *Context) SetSession(s Session) { c.session = s }

// Auth returns the authentication guard (HTTP-09). Nil when no guard is
// installed; use ctx.User() or the auth middleware for the common cases.
func (c *Context) Auth() Auth { return c.auth }

// SetAuth installs an authentication guard. Called by the auth middleware.
func (c *Context) SetAuth(a Auth) { c.auth = a }

// User returns the authenticated user, or nil when the request is a guest.
func (c *Context) User() any {
	if c.auth == nil {
		return nil
	}
	return c.auth.User()
}

// Errors returns the request's validation errors, if any were flashed by a
// previous request (VIEW-08).
func (c *Context) Errors() ErrorBag {
	if c.errors == nil {
		c.errors = ErrorBag{}
	}
	return c.errors
}

// SetErrors installs validation errors, used by the shared-props middleware when
// it pulls flashed errors out of the session.
func (c *Context) SetErrors(bag ErrorBag) {
	if bag == nil {
		bag = ErrorBag{}
	}
	c.errors = bag
}

// Share adds props shared with every page rendered during this request. The
// ShareProps middleware uses it for the authenticated user, flash messages and
// the CSRF token.
func (c *Context) Share(props Props) { c.shared = c.shared.Merge(props) }

// Shared returns the props shared with every page on this request.
func (c *Context) Shared() Props { return c.shared }

// ---------------------------------------------------------------------------
// Responses
// ---------------------------------------------------------------------------

// Status sets the response status code used by the next write. Returns the
// context for chaining.
func (c *Context) Status(code int) *Context {
	c.status = code
	return c
}

// StatusCode returns the status currently set, or 200 when nothing was set.
func (c *Context) StatusCode() int {
	if c.status == 0 {
		return http.StatusOK
	}
	return c.status
}

// Written reports whether the response has already been written. Middleware and
// the error handler use it to avoid double-writing.
func (c *Context) Written() bool { return c.written }

// JSON writes v as a JSON response.
func (c *Context) JSON(v any) error {
	c.SaveSession()
	c.response.Header().Set("Content-Type", ContentTypeJSON)
	c.response.WriteHeader(c.StatusCode())
	c.written = true
	enc := json.NewEncoder(c.response)
	return enc.Encode(v)
}

// JSONWith writes v as JSON with an explicit status code.
func (c *Context) JSONWith(code int, v any) error {
	return c.Status(code).JSON(v)
}

// String writes a plain text response.
func (c *Context) String(body string) error {
	c.SaveSession()
	c.response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.response.WriteHeader(c.StatusCode())
	c.written = true
	_, err := io.WriteString(c.response, body)
	return err
}

// NoContent writes an empty 204 response.
func (c *Context) NoContent() error {
	c.SaveSession()
	c.response.WriteHeader(http.StatusNoContent)
	c.written = true
	return nil
}

// ---------------------------------------------------------------------------
// Redirects (VIEW-07)
// ---------------------------------------------------------------------------

// Redirect begins a redirect to the given URL. Chain With to flash data:
//
//	return ctx.Redirect("/posts").With("success", "Post created")
func (c *Context) Redirect(to string) *Redirect {
	return &Redirect{ctx: c, url: to, status: http.StatusFound}
}

// RedirectPermanent begins a 301 redirect.
func (c *Context) RedirectPermanent(to string) *Redirect {
	return &Redirect{ctx: c, url: to, status: http.StatusMovedPermanently}
}

// RedirectRoute begins a redirect to a named route (HTTP-03).
func (c *Context) RedirectRoute(name string, params ...any) *Redirect {
	return c.Redirect(c.app.URL(name, params...))
}

// Back begins a redirect to the previous URL, falling back to "/" when no
// referer or intended URL is known.
func (c *Context) Back() *Redirect {
	return &Redirect{ctx: c, back: true, status: http.StatusFound}
}

// BackWithErrors begins a redirect back to the previous page with the given
// validation errors (VIEW-08). This is the counterpart to ctx.Validate:
//
//	var req requests.StorePost
//	if err := ctx.Validate(&req); err != nil {
//		return ctx.BackWithErrors(err)
//	}
func (c *Context) BackWithErrors(bag ErrorBag) *Redirect {
	return c.Back().WithErrors(bag)
}

// BackWithErrorsIn is BackWithErrors for a named error bag, for pages that host
// more than one form (VIEW-08).
func (c *Context) BackWithErrorsIn(name string, bag ErrorBag) *Redirect {
	return c.Back().WithErrorsIn(name, bag)
}

// intendedSessionKey stores the URL a blocked request was trying to reach.
const intendedSessionKey = "url.intended"

// Intended returns the URL recorded by Redirect().Intended() and clears it, so
// it is consumed once. It falls back to def when nothing was recorded.
//
// Use it after a successful login:
//
//	return ctx.Redirect(ctx.Intended("/dashboard")).With("success", "Welcome back")
func (c *Context) Intended(def string) string {
	session := c.Session()
	target := session.GetString(intendedSessionKey, "")
	if target == "" {
		return def
	}
	session.Remove(intendedSessionKey)
	return target
}

// Location aborts the request with an external redirect, following the Inertia
// convention: a 409 with an X-Vuepher-Location header, which makes the client
// perform a full page load. Use it for links that leave the Vue application.
func (c *Context) Location(url string) error {
	if c.IsPageRequest() {
		return Abort(http.StatusConflict, "").WithHeader(HeaderLocation, url)
	}
	c.response.Header().Set("Location", url)
	return c.RedirectPermanent(url)
}

// ---------------------------------------------------------------------------
// Rendering (VIEW-01)
// ---------------------------------------------------------------------------

// View renders a Vue page component with the given props. The same props are
// also merged into the shared props so that layout-level data (flash messages,
// the authenticated user) is sent exactly once.
//
// On the first, non-SPA visit it writes a full HTML document. On an SPA visit it
// writes the JSON page object.
func (c *Context) View(component string, props Props) error {
	if c.app == nil || c.app.Renderer == nil {
		return Internal("no view renderer is configured", nil)
	}
	return c.app.Renderer.Render(c, component, props)
}

// ViewTyped renders a page and records the props type name so that type
// generation can emit a PageProps declaration for the component (TG-05).
//
// This is a package-level generic function rather than a method because Go does
// not permit type parameters on methods:
//
//	return vuepher.View[PostsIndexProps](ctx, "Posts/Index", vuepher.Props{"posts": posts})
//
// The type T is not used at runtime beyond its name; it exists so that
// type generation and, in future, generated controller signatures stay in sync
// with the component.
func View[T any](c *Context, component string, props ...Props) error {
	merged := Props{}
	if len(props) > 0 {
		merged = props[0]
	}
	if c.app != nil {
		c.app.Renderer.recordPropsType(component, typeName[T]())
	}
	return c.View(component, merged)
}

// typeName returns the Go type name of T, used by type generation to bind a
// component to its props struct. Falls back to the reflected type string for
// unnamed composite types.
func typeName[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return ""
	}
	if name := t.Name(); name != "" {
		return name
	}
	return t.String()
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// Validate binds the request payload into dst and validates it against the
// struct's `validate` tags (HTTP-07).
//
// It returns an ErrorBag, which is non-nil only when validation failed:
//
//	var req requests.StorePost
//	if err := ctx.Validate(&req); err != nil {
//		return ctx.BackWithErrors(err)
//	}
//
// Note that the return type is the concrete ErrorBag rather than error, so that
// a successful call compares equal to nil without the usual interface-nil trap.
func (c *Context) Validate(dst any) ErrorBag {
	if err := c.Bind(dst); err != nil {
		return ErrorBag{"_request": MessageOf(err)}
	}
	if c.app == nil || c.app.Validator == nil {
		return nil
	}
	return c.app.Validator.Validate(dst)
}

// ValidateOnly validates dst without binding the request into it. Useful when a
// handler has already populated the struct from route parameters.
func (c *Context) ValidateOnly(dst any) ErrorBag {
	if c.app == nil || c.app.Validator == nil {
		return nil
	}
	return c.app.Validator.Validate(dst)
}

// ---------------------------------------------------------------------------
// Plumbing
// ---------------------------------------------------------------------------

// SaveSession persists pending session state. It must run before the response
// headers are written, because a session cookie can only be set while the header
// map is still mutable.
//
// The framework calls it automatically from every write path (JSON, String,
// NoContent, View, Redirect) and it is idempotent, so a request that redirects
// after flashing data still sets exactly one cookie. Middleware that writes a
// response itself must call it first; failures are reported to the application
// log rather than returned, because a session that cannot be saved is not a
// reason to fail an otherwise valid response.
func (c *Context) SaveSession() {
	if c.app == nil || c.session == nil || c.sessionSaved {
		return
	}
	if err := c.app.saveSession(c, c.session); err != nil {
		c.app.reportError(err)
	}
}

// withContext stashes the Context on the request so net/http code can find it.
func (c *Context) withContext() *Context {
	c.request = c.request.WithContext(context.WithValue(c.request.Context(), ctxKey, c))
	return c
}

// FromRequest recovers the Vuepher Context from a request, or nil when the
// request did not come through a Vuepher router.
func FromRequest(r *http.Request) *Context {
	if r == nil {
		return nil
	}
	c, _ := r.Context().Value(ctxKey).(*Context)
	return c
}

// toString renders a decoded JSON scalar as a string for input coercion.
func toString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case json.Number:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	default:
		buf, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(buf)
	}
}
