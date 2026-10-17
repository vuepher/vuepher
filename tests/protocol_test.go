// Package tests holds black-box tests for the framework. They build a real
// application, serve it through httptest and assert the wire contract that the
// @vuepher/vue3 client depends on (PRD §5.2, risk R4).
//
// Nothing here reaches into framework internals: if a change breaks these tests,
// an application that follows the documented protocol would break with it.
package tests

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/middleware"
)

// testRootTemplate mirrors the root template `vuepher new` writes, so the tests
// exercise the same placeholders a generated application uses (VIEW-02).
const testRootTemplate = `<!DOCTYPE html>
<html lang="{{ .VuepherLang }}">
<head>
    <meta charset="utf-8">
    <title>{{ .Title }}</title>
    {{ .VuepherStyleTags }}
</head>
<body>
    {{ .VuepherApp }}
    {{ .VuepherScriptTags }}
</body>
</html>
`

// testManifest is a minimal Vite manifest. Its bytes are hashed into the asset
// version, which is what the version-mismatch test compares against.
const testManifest = `{
  "resources/js/app.ts": {
    "file": "assets/app-4f3c1a.js",
    "src": "resources/js/app.ts",
    "isEntry": true,
    "css": ["assets/app-8b2e77.css"]
  }
}`

// harness is a running test application plus a cookie-aware client.
type harness struct {
	t      *testing.T
	server *httptest.Server
	app    *vuepher.App
	client *http.Client

	// version is the asset version the server reports.
	version string

	// stored records the payload of the last accepted POST /posts.
	stored map[string]any
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	fsys := fstest.MapFS{
		"resources/views/app.html":         &fstest.MapFile{Data: []byte(testRootTemplate)},
		"public/build/.vite/manifest.json": &fstest.MapFile{Data: []byte(testManifest)},
	}
	resolver, err := vuepher.NewManifestAssetResolver(
		fsys, "public/build/.vite/manifest.json", "/build/", "resources/js/app.ts",
	)
	if err != nil {
		t.Fatalf("could not load the test manifest: %v", err)
	}

	cfg := &vuepher.Config{
		App: vuepher.AppConfig{
			Name:   "Vuepher Test",
			Env:    "testing",
			Key:    strings.Repeat("k", 32),
			Locale: "en",
		},
		Session: vuepher.SessionConfig{
			Driver:          "memory",
			Name:            "vuepher_session",
			LifetimeMinutes: 120,
			SameSite:        http.SameSiteLaxMode,
		},
	}

	h := &harness{t: t, app: vuepher.NewApp(cfg), version: resolver.Version()}

	// The same middleware order bootstrap/app.go uses: Session before ShareProps,
	// because ShareProps reads flash data and the CSRF token out of the session.
	h.app.Use(
		middleware.Session(h.app.Sessions),
		middleware.ShareProps(),
		middleware.Recover(h.app.Logger),
	)

	if err := h.app.UseRenderer(vuepher.RendererOptions{
		FS:           fsys,
		RootTemplate: "resources/views/app.html",
		Title:        "Vuepher Test",
		Assets:       resolver,
	}); err != nil {
		t.Fatalf("could not install the renderer: %v", err)
	}
	h.app.Renderer.SetErrorComponent(403, "Errors/403")
	h.app.Renderer.SetErrorComponent(404, "Errors/404")
	h.app.Renderer.SetErrorComponent(500, "Errors/500")

	h.registerRoutes()

	h.server = httptest.NewServer(h.app)
	t.Cleanup(h.server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("could not build a cookie jar: %v", err)
	}
	h.client = &http.Client{
		Jar: jar,
		// Redirects are part of the contract under test (VIEW-01 step 6), so they
		// must be observable rather than followed. Cookies from a 303 are still
		// stored in the jar, which is what lets a test assert the flash data.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return h
}

// registerRoutes wires the smallest application that exercises every branch of
// the view bridge.
func (h *harness) registerRoutes() {
	h.app.Get("/", func(ctx *vuepher.Context) error {
		return ctx.View("Home", vuepher.Props{"message": "Hello"})
	})

	// Index carries two props so a partial reload has something to leave out.
	h.app.Get("/posts", func(ctx *vuepher.Context) error {
		return ctx.View("Posts/Index", vuepher.Props{
			"posts": []map[string]any{
				{"id": 1, "title": "Hello, Vuepher"},
				{"id": 2, "title": "Rendering Vue pages from Go"},
			},
			"stats": map[string]any{"count": 2},
		})
	})

	h.app.Get("/posts/create", func(ctx *vuepher.Context) error {
		return ctx.View("Posts/Create", vuepher.Props{})
	})

	h.app.Post("/posts", func(ctx *vuepher.Context) error {
		var req struct {
			Title string `json:"title" validate:"required,min=3,max=120"`
			Body  string `json:"body" validate:"required"`
		}
		if err := ctx.Validate(&req); err != nil {
			return ctx.BackWithErrors(err)
		}
		h.stored = map[string]any{"title": req.Title, "body": req.Body}
		return ctx.Redirect("/posts").With("success", "Post created.")
	})

	// A route that makes the framework render an error page for a 404.
	h.app.Get("/missing-post", func(ctx *vuepher.Context) error {
		return vuepher.NotFound("That post does not exist.")
	})

	// The message contains markup and a quote, which must survive the trip
	// through the data-page attribute without breaking it (R9).
	h.app.Get("/escaped-error", func(ctx *vuepher.Context) error {
		return vuepher.NotFound(escapeProbe)
	})

	h.app.Get("/boom", func(ctx *vuepher.Context) error {
		panic("the controller exploded")
	})

	// A resource route, to assert the naming convention (HTTP-02).
	h.app.Resource("articles", &articlesController{})
	h.app.RegisterHealth("/up")
}

// escapeProbe is the string the escaping tests look for.
const escapeProbe = `Missing </script><script>alert("x")</script> & <b>bold</b>`

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

type request struct {
	method  string
	path    string
	body    string
	headers map[string]string
}

func (h *harness) do(req request) (*http.Response, string) {
	h.t.Helper()

	var body io.Reader
	if req.body != "" {
		body = strings.NewReader(req.body)
	}
	httpReq, err := http.NewRequest(req.method, h.server.URL+req.path, body)
	if err != nil {
		h.t.Fatalf("could not build the request: %v", err)
	}
	if req.body != "" {
		httpReq.Header.Set("Content-Type", vuepher.ContentTypeJSON)
	}
	for key, value := range req.headers {
		httpReq.Header.Set(key, value)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		h.t.Fatalf("%s %s failed: %v", req.method, req.path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("could not read the response body: %v", err)
	}
	return resp, string(data)
}

// embeddedPage extracts the page object the server embedded in the HTML.
//
// The attribute is HTML-escaped by the server and decoded by the browser, so the
// test decodes it the same way: this is the step that catches an escaping scheme
// the client cannot read back.
func embeddedPage(t *testing.T, body string) vuepher.PageObject {
	t.Helper()

	const marker = `data-page="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("the response has no data-page attribute:\n%s", body)
	}
	start += len(marker)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatalf("the data-page attribute is not terminated:\n%s", body)
	}
	raw := body[start : start+end]

	var page vuepher.PageObject
	if err := json.Unmarshal([]byte(html.UnescapeString(raw)), &page); err != nil {
		t.Fatalf("the embedded page object is not readable back as JSON: %v\nraw: %s", err, raw)
	}
	return page
}

// jsonPage decodes an SPA response body.
func jsonPage(t *testing.T, body string) vuepher.PageObject {
	t.Helper()
	var page vuepher.PageObject
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the page object response is not valid JSON: %v\nbody: %s", err, body)
	}
	return page
}

// prop reads a prop and fails the test when it is missing.
func prop(t *testing.T, page vuepher.PageObject, key string) any {
	t.Helper()
	value, ok := page.Props[key]
	if !ok {
		t.Fatalf("props are missing %q; got keys %v", key, propKeys(page.Props))
	}
	return value
}

func propKeys(props vuepher.Props) []string {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	return keys
}

// ---------------------------------------------------------------------------
// VIEW-01: first visit is HTML, SPA visit is a page object
// ---------------------------------------------------------------------------

func TestFirstVisitRendersHTMLDocumentWithPageObject(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/posts"})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, vuepher.ContentTypeHTML) {
		t.Fatalf("Content-Type = %q, want %q", got, vuepher.ContentTypeHTML)
	}
	if !strings.Contains(body, `<div id="app" data-page=`) {
		t.Fatalf("the document has no app root element:\n%s", body)
	}
	// The build's hashed assets must be linked, or a production page loads no
	// JavaScript at all (VIEW-03).
	for _, want := range []string{"/build/assets/app-4f3c1a.js", "/build/assets/app-8b2e77.css"} {
		if !strings.Contains(body, want) {
			t.Errorf("the document does not reference %s:\n%s", want, body)
		}
	}

	page := embeddedPage(t, body)
	if page.Component != "Posts/Index" {
		t.Errorf("component = %q, want Posts/Index", page.Component)
	}
	if page.URL != "/posts" {
		t.Errorf("url = %q, want /posts", page.URL)
	}
	if page.Version != h.version {
		t.Errorf("version = %q, want the asset version %q", page.Version, h.version)
	}
	posts, ok := prop(t, page, "posts").([]any)
	if !ok || len(posts) != 2 {
		t.Errorf("props.posts = %#v, want a list of 2", page.Props["posts"])
	}
}

func TestSPAVisitReturnsJSONPageObject(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{
		method:  http.MethodGet,
		path:    "/posts",
		headers: map[string]string{vuepher.HeaderRequest: "true"},
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, vuepher.ContentTypeJSON) {
		t.Fatalf("Content-Type = %q, want %q", got, vuepher.ContentTypeJSON)
	}
	if strings.Contains(body, "<html") {
		t.Fatalf("an SPA visit returned a document instead of a page object:\n%s", body)
	}

	page := jsonPage(t, body)
	if page.Component != "Posts/Index" {
		t.Errorf("component = %q, want Posts/Index", page.Component)
	}
	if page.URL != "/posts" {
		t.Errorf("url = %q, want /posts", page.URL)
	}
	// Shared props are merged into every page object, which is how the layout
	// gets flash data and the CSRF token without the controller mentioning them
	// (VIEW-05).
	for _, key := range []string{"flash", "errors", "old", "csrf_token"} {
		prop(t, page, key)
	}
}

// ---------------------------------------------------------------------------
// VIEW-04: an asset version mismatch forces a full reload
// ---------------------------------------------------------------------------

func TestStaleAssetVersionAnswersConflictWithLocation(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{
		method: http.MethodGet,
		path:   "/posts",
		headers: map[string]string{
			vuepher.HeaderRequest: "true",
			vuepher.HeaderVersion: "stale-build",
		},
	})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if got := resp.Header.Get(vuepher.HeaderLocation); got != "/posts" {
		t.Errorf("%s = %q, want /posts", vuepher.HeaderLocation, got)
	}
	if body != "" {
		t.Errorf("a 409 must carry no body, got %q", body)
	}
}

func TestMatchingAssetVersionIsServed(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(request{
		method: http.MethodGet,
		path:   "/posts",
		headers: map[string]string{
			vuepher.HeaderRequest: "true",
			vuepher.HeaderVersion: h.version,
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// VIEW-06: partial reloads
// ---------------------------------------------------------------------------

func TestPartialReloadIncludesOnlyTheRequestedProps(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{
		method: http.MethodGet,
		path:   "/posts",
		headers: map[string]string{
			vuepher.HeaderRequest:          "true",
			vuepher.HeaderPartialData:      "true",
			vuepher.HeaderPartialComponent: "Posts/Index",
			vuepher.HeaderPartialOnly:      "posts",
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	page := jsonPage(t, body)
	if _, ok := page.Props["posts"]; !ok {
		t.Errorf("props are missing the requested key; got %v", propKeys(page.Props))
	}
	if _, ok := page.Props["stats"]; ok {
		t.Errorf("a partial reload returned a prop that was not requested: %v", propKeys(page.Props))
	}
}

func TestPartialReloadExceptOmitsTheNamedProps(t *testing.T) {
	h := newHarness(t)
	_, body := h.do(request{
		method: http.MethodGet,
		path:   "/posts",
		headers: map[string]string{
			vuepher.HeaderRequest:          "true",
			vuepher.HeaderPartialData:      "true",
			vuepher.HeaderPartialComponent: "Posts/Index",
			vuepher.HeaderPartialExcept:    "stats",
		},
	})

	page := jsonPage(t, body)
	if _, ok := page.Props["posts"]; !ok {
		t.Errorf("props are missing posts; got %v", propKeys(page.Props))
	}
	if _, ok := page.Props["stats"]; ok {
		t.Errorf("props still contain the excluded key: %v", propKeys(page.Props))
	}
}

func TestPartialReloadForAnotherComponentIsServedInFull(t *testing.T) {
	h := newHarness(t)
	_, body := h.do(request{
		method: http.MethodGet,
		path:   "/posts",
		headers: map[string]string{
			vuepher.HeaderRequest:          "true",
			vuepher.HeaderPartialData:      "true",
			vuepher.HeaderPartialComponent: "Somewhere/Else",
			vuepher.HeaderPartialOnly:      "posts",
		},
	})

	page := jsonPage(t, body)
	if _, ok := page.Props["stats"]; !ok {
		t.Errorf("a partial reload aimed at another component must be answered in full; got %v", propKeys(page.Props))
	}
}

// ---------------------------------------------------------------------------
// VIEW-01 step 6, VIEW-05, VIEW-07, VIEW-08: writes, redirects and flash
// ---------------------------------------------------------------------------

func TestPostRedirectsWithSeeOtherAndFlashesSuccess(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(request{
		method: http.MethodPost,
		path:   "/posts",
		body:   `{"title":"A valid title","body":"Body text"}`,
	})

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 so the browser repeats the request as a GET", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/posts" {
		t.Errorf("Location = %q, want /posts", got)
	}
	if h.stored == nil || h.stored["title"] != "A valid title" {
		t.Fatalf("the controller did not receive the bound payload: %#v", h.stored)
	}

	// The flash message travels on the session cookie set by the redirect.
	_, body := h.do(request{
		method:  http.MethodGet,
		path:    "/posts",
		headers: map[string]string{vuepher.HeaderRequest: "true"},
	})
	page := jsonPage(t, body)
	flash, ok := prop(t, page, "flash").(map[string]any)
	if !ok || flash["success"] != "Post created." {
		t.Fatalf("flash = %#v, want success = %q", page.Props["flash"], "Post created.")
	}
}

func TestValidationErrorsAreFlashedAsPropsWithOldInput(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.do(request{
		method: http.MethodPost,
		path:   "/posts",
		body:   `{"title":"no","body":""}`,
		headers: map[string]string{
			"Referer": h.server.URL + "/posts/create",
		},
	})

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "/posts/create" {
		t.Errorf("Location = %q, want the referer /posts/create", got)
	}

	// The next page must carry the errors and the input that produced them.
	_, body := h.do(request{
		method:  http.MethodGet,
		path:    "/posts/create",
		headers: map[string]string{vuepher.HeaderRequest: "true"},
	})
	page := jsonPage(t, body)

	errors, ok := prop(t, page, "errors").(map[string]any)
	if !ok {
		t.Fatalf("errors = %#v, want an object", page.Props["errors"])
	}
	for _, field := range []string{"title", "body"} {
		if _, ok := errors[field]; !ok {
			t.Errorf("errors are missing %q; got %v", field, errors)
		}
	}

	old, ok := prop(t, page, "old").(map[string]any)
	if !ok {
		t.Fatalf("old = %#v, want an object", page.Props["old"])
	}
	if old["title"] != "no" {
		t.Errorf("old input title = %#v, want the submitted value %q", old["title"], "no")
	}
}

func TestJSONBodyIgnoresUnknownFields(t *testing.T) {
	h := newHarness(t)
	// A client that sends one extra field must still be served: the form path
	// has always ignored unknown fields, and JSON must behave the same way.
	resp, _ := h.do(request{
		method: http.MethodPost,
		path:   "/posts",
		body:   `{"title":"A valid title","body":"Body text","extra":{"nested":true}}`,
	})

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; a request with an extra field must not fail to decode", resp.StatusCode)
	}
	if h.stored == nil || h.stored["body"] != "Body text" {
		t.Fatalf("the payload was not bound: %#v", h.stored)
	}
}

// ---------------------------------------------------------------------------
// VIEW-09, HTTP-11: error pages are Vue components
// ---------------------------------------------------------------------------

func TestUnknownRouteRendersTheVueNotFoundPage(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/no-such-page"})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	page := embeddedPage(t, body)
	if page.Component != "Errors/404" {
		t.Errorf("component = %q, want Errors/404", page.Component)
	}
	if status, _ := page.Props["status"].(float64); status != 404 {
		t.Errorf("props.status = %#v, want 404", page.Props["status"])
	}
}

func TestUnknownRouteStillAnswersAPageObjectForSPAVisits(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{
		method:  http.MethodGet,
		path:    "/no-such-page",
		headers: map[string]string{vuepher.HeaderRequest: "true"},
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	// An error page is always a full document: it may have been raised before the
	// client could handle a page object, so the client must be able to reload.
	if !strings.Contains(body, "data-page=") {
		t.Fatalf("the error response is not a document:\n%s", body)
	}
	if page := embeddedPage(t, body); page.Component != "Errors/404" {
		t.Errorf("component = %q, want Errors/404", page.Component)
	}
}

func TestMethodNotAllowedRendersAClientErrorPage(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodDelete, path: "/posts"})

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if page := embeddedPage(t, body); page.Component != "Errors/404" {
		t.Errorf("component = %q, want the 4xx page Errors/404", page.Component)
	}
}

func TestNotFoundRaisedByAControllerRendersTheErrorPage(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/missing-post"})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	page := embeddedPage(t, body)
	if page.Props["message"] != "That post does not exist." {
		t.Errorf("props.message = %#v, want the controller's message", page.Props["message"])
	}
}

func TestPanicIsRecoveredAsAServerErrorPage(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/boom"})

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if page := embeddedPage(t, body); page.Component != "Errors/500" {
		t.Errorf("component = %q, want Errors/500", page.Component)
	}
}

// TestEmbeddedPageObjectSurvivesMarkupInProps covers R9: a prop containing a
// closing tag or a quote must not be able to break out of the attribute, and the
// value must still arrive intact on the other side.
func TestEmbeddedPageObjectSurvivesMarkupInProps(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/escaped-error"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	page := embeddedPage(t, body)
	if page.Props["message"] != escapeProbe {
		t.Errorf("props.message did not survive escaping:\n got %q\nwant %q", page.Props["message"], escapeProbe)
	}

	// The raw attribute value must not contain a character the HTML parser would
	// treat as markup.
	const marker = `data-page="`
	start := strings.Index(body, marker) + len(marker)
	end := start + strings.Index(body[start:], `"`)
	raw := body[start:end]
	for _, forbidden := range []string{"<", ">", `"`} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the data-page attribute contains a raw %q, which the browser would read as markup", forbidden)
		}
	}
}

func TestHealthEndpointReportsOK(t *testing.T) {
	h := newHarness(t)
	resp, body := h.do(request{method: http.MethodGet, path: "/up"})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("the health endpoint did not answer JSON: %v\n%s", err, body)
	}
	if payload["status"] != "ok" {
		t.Errorf("status = %#v, want ok", payload["status"])
	}
}

// ---------------------------------------------------------------------------
// HTTP-02/HTTP-03: resource routes follow the Laravel convention
// ---------------------------------------------------------------------------

func TestResourceRoutesUseTheConventionalNames(t *testing.T) {
	h := newHarness(t)

	want := map[string]string{
		"article.index":   "GET /articles",
		"article.create":  "GET /articles/create",
		"article.store":   "POST /articles",
		"article.show":    "GET /articles/{id}",
		"article.edit":    "GET /articles/{id}/edit",
		"article.update":  "PUT /articles/{id}",
		"article.destroy": "DELETE /articles/{id}",
	}

	got := map[string]string{}
	for _, info := range h.app.RouteList() {
		if info.Name == "" {
			continue
		}
		got[info.Name] = info.Method + " " + info.Path
	}
	for name, route := range want {
		if got[name] != route {
			t.Errorf("route %s = %q, want %q", name, got[name], route)
		}
	}

	if url := h.app.URL("article.show", 7); url != "/articles/7" {
		t.Errorf("URL(article.show, 7) = %q, want /articles/7", url)
	}
}

// articlesController implements all seven resource actions, which is what makes
// router.Resource register all seven routes.
type articlesController struct{}

func (c *articlesController) Index(ctx *vuepher.Context) error {
	return ctx.View("Articles/Index", vuepher.Props{})
}

func (c *articlesController) Create(ctx *vuepher.Context) error {
	return ctx.View("Articles/Create", vuepher.Props{})
}

func (c *articlesController) Store(ctx *vuepher.Context) error {
	return ctx.Redirect("/articles")
}

func (c *articlesController) Show(ctx *vuepher.Context) error {
	return ctx.View("Articles/Show", vuepher.Props{})
}

func (c *articlesController) Edit(ctx *vuepher.Context) error {
	return ctx.View("Articles/Edit", vuepher.Props{})
}

func (c *articlesController) Update(ctx *vuepher.Context) error {
	return ctx.Redirect("/articles")
}

func (c *articlesController) Destroy(ctx *vuepher.Context) error {
	return ctx.Redirect("/articles")
}
