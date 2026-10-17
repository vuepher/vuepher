package vuepher

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sync"
)

// Renderer turns a component name plus props into an HTTP response: a full HTML
// document on a first visit, a JSON page object on an SPA visit (VIEW-01).
//
// The default implementation renders the application's root template, which must
// contain the placeholders {{ .VuepherApp }} and {{ .VuepherAssets }} (VIEW-02).
type Renderer struct {
	app *App

	mu           sync.RWMutex
	fsys         fs.FS
	rootTemplate string
	tmpl         *template.Template
	assets       AssetResolver

	// title is the default document title, overridable per request with
	// ctx.Share(vuepher.Props{"meta": ...}).
	title string

	// errorComponents maps a status code to the Vue component that renders it
	// (VIEW-09).
	errorComponents map[int]string

	// propsTypes records which Go type backs each component's props, populated
	// by vuepher.View[T] and consumed by type generation (TG-05).
	propsTypes map[string]string
}

// RendererOptions configure the default renderer.
type RendererOptions struct {
	// FS is the filesystem holding the root template and, in production, the
	// Vite manifest. Use os.DirFS for development and an embedded FS for a
	// single-binary build.
	FS fs.FS

	// RootTemplate is the template path inside FS, e.g. "app.html".
	RootTemplate string

	// Title is the default document title.
	Title string

	// Assets overrides asset resolution. When nil, an AssetResolver is chosen
	// from the Vite configuration.
	Assets AssetResolver
}

// NewRenderer builds the default renderer and parses the root template.
func NewRenderer(app *App, options RendererOptions) (*Renderer, error) {
	if options.FS == nil {
		return nil, fmt.Errorf("vuepher: renderer needs a template filesystem (RendererOptions.FS)")
	}
	if options.RootTemplate == "" {
		options.RootTemplate = "app.html"
	}
	r := &Renderer{
		app:             app,
		fsys:            options.FS,
		rootTemplate:    options.RootTemplate,
		title:           options.Title,
		errorComponents: defaultErrorComponents(),
		propsTypes:      map[string]string{},
	}
	if r.title == "" {
		r.title = app.Config.App.Name
	}
	if options.Assets != nil {
		r.assets = options.Assets
	} else {
		resolver, err := NewAssetResolver(app.Config.Vite, options.FS)
		if err != nil {
			return nil, err
		}
		r.assets = resolver
	}
	if err := r.Parse(); err != nil {
		return nil, err
	}
	return r, nil
}

// defaultErrorComponents maps status codes to Vue components (VIEW-09).
func defaultErrorComponents() map[int]string {
	return map[int]string{
		http.StatusForbidden:           "Errors/403",
		http.StatusNotFound:            "Errors/404",
		http.StatusTooManyRequests:     "Errors/429",
		http.StatusServiceUnavailable:  "Errors/503",
		http.StatusInternalServerError: "Errors/500",
	}
}

// rootTemplateName is the name the root template is registered under.
//
// The file is read and parsed explicitly rather than through template.ParseFS,
// because ParseFS names a template after the base name of the file: the template
// would be registered as "app.html" while the renderer looks it up as
// "resources/views/app.html", and every page render would fail with "no such
// template". Reading the file also lets the missing-file error name the path it
// looked for.
const rootTemplateName = "vuepher-root"

// Parse (re)loads the root template. Called at boot and by the dev server when
// the template file changes.
//
// TODO(phase-2): support {{ template "partial" }} includes by parsing every
// template in the views directory alongside the root.
func (r *Renderer) Parse() error {
	content, err := fs.ReadFile(r.fsys, r.rootTemplate)
	if err != nil {
		return fmt.Errorf(
			"vuepher: could not read the root template %s: %w\n"+
				"  It must exist and contain the placeholders {{ .VuepherApp }} and "+
				"{{ .VuepherStyleTags }}/{{ .VuepherScriptTags }}.",
			r.rootTemplate, err,
		)
	}

	tmpl, err := template.New(rootTemplateName).Parse(string(content))
	if err != nil {
		return fmt.Errorf(
			"vuepher: could not parse the root template %s: %w\n"+
				"  It must contain the placeholders {{ .VuepherApp }} and "+
				"{{ .VuepherStyleTags }}/{{ .VuepherScriptTags }}.",
			r.rootTemplate, err,
		)
	}

	r.mu.Lock()
	r.tmpl = tmpl
	r.mu.Unlock()
	return nil
}

// SetAssets swaps the asset resolver, used when the dev server starts or stops.
func (r *Renderer) SetAssets(resolver AssetResolver) {
	r.mu.Lock()
	r.assets = resolver
	r.mu.Unlock()
}

// Assets returns the current asset resolver.
func (r *Renderer) Assets() AssetResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.assets
}

// SetErrorComponent overrides the component rendered for a status code
// (VIEW-09).
func (r *Renderer) SetErrorComponent(status int, component string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.errorComponents == nil {
		r.errorComponents = map[int]string{}
	}
	r.errorComponents[status] = component
}

// ErrorComponent returns the component for a status code, walking up to a
// generic entry when a specific page is not configured: the 404 page for a
// client error, the 500 page for a server error.
//
// "You asked for something that is not here" fits any 4xx better than "the
// server broke", and a 405 or 429 rendered with the 500 component would read as
// an outage to the user.
func (r *Renderer) ErrorComponent(status int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if component, ok := r.errorComponents[status]; ok {
		return component
	}
	if status < http.StatusInternalServerError {
		return r.errorComponents[http.StatusNotFound]
	}
	return r.errorComponents[http.StatusInternalServerError]
}

// recordPropsType remembers the props struct behind a component (TG-05).
func (r *Renderer) recordPropsType(component, typeName string) {
	if typeName == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.propsTypes[component] = typeName
}

// PropsTypes returns a copy of the component -> props type mapping, which the
// CLI writes out as TypeScript declarations.
func (r *Renderer) PropsTypes() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.propsTypes))
	for component, typeName := range r.propsTypes {
		out[component] = typeName
	}
	return out
}

// Render writes the response for a page (VIEW-01).
func (r *Renderer) Render(ctx *Context, component string, props Props) error {
	assets := r.Assets()
	version := ""
	if assets != nil {
		version = assets.Version()
	}

	// Refuse to render a document that cannot load any JavaScript, and say why.
	// This is the failure a developer hits when they run the server before
	// building the frontend for the first time.
	if missing, ok := assets.(*MissingAssetsResolver); ok {
		return Internal(missing.Reason, nil)
	}

	pr := ctx.pageRequest()

	// A client holding a different asset build is asked to reload fully
	// (VIEW-04). This happens after a deploy while a tab is still open.
	if pr.Version != "" && version != "" && pr.Version != version {
		location := ctx.URL()
		ctx.SaveSession()
		ctx.response.Header().Set(HeaderLocation, location)
		ctx.response.WriteHeader(http.StatusConflict)
		ctx.written = true
		return nil
	}

	// Deferred props are resolved before merging so that the client can be told
	// which ones were included (VIEW-06).
	resolvedProps, deferred, err := resolveDeferred(props)
	if err != nil {
		return Internal("a deferred prop could not be resolved", err)
	}

	merged := ctx.Shared().Merge(resolvedProps)

	// A partial reload addressed at a different component is served in full:
	// the user was redirected somewhere they did not ask for.
	if pr.IsPartial && pr.TakesComponent(component) {
		merged = pr.Apply(merged)
	}

	page := &PageObject{
		Component:     component,
		Props:         merged,
		URL:           ctx.URL(),
		Version:       version,
		DeferredProps: deferred,
	}

	if pr.IsPageRequest {
		return r.renderJSON(ctx, page)
	}
	return r.renderHTML(ctx, page)
}

// renderJSON writes the page object as the response body.
func (r *Renderer) renderJSON(ctx *Context, page *PageObject) error {
	return ctx.JSON(page)
}

// renderHTML renders the root template with the page object embedded in the
// data-page attribute.
func (r *Renderer) renderHTML(ctx *Context, page *PageObject) error {
	r.mu.RLock()
	tmpl := r.tmpl
	r.mu.RUnlock()

	if tmpl == nil {
		return Internal("the root template has not been parsed", nil)
	}

	data, err := r.buildTemplateData(page)
	if err != nil {
		return Internal(fmt.Sprintf("the props for component %q could not be serialised", page.Component), err)
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, rootTemplateName, data); err != nil {
		return Internal("the root template could not be rendered", err)
	}

	// The session cookie has to be set before the response headers go out.
	ctx.SaveSession()
	ctx.response.Header().Set("Content-Type", ContentTypeHTML)
	ctx.response.WriteHeader(ctx.StatusCode())
	ctx.written = true
	_, err = ctx.response.Write(buf.Bytes())
	return err
}

// RenderError renders an error page as a Vue component (VIEW-09). In debug mode
// the error detail is included as the `debug` prop; in production only the
// status and message are sent.
func (r *Renderer) RenderError(ctx *Context, err error) error {
	status := StatusOf(err)
	component := r.ErrorComponent(status)
	if component == "" {
		// No error components configured: fall back to a plain text response so
		// the application still behaves.
		ctx.SaveSession()
		http.Error(ctx.response, MessageOf(err), status)
		ctx.written = true
		return nil
	}

	props := Props{
		"status":  status,
		"message": MessageOf(err),
	}
	if r.app.Config.App.Debug {
		props["debug"] = fmt.Sprintf("%+v", err)
	}

	// Error pages always render the full document, even for SPA visits: an
	// error may have occurred before the client could handle a page object.
	page := &PageObject{
		Component: component,
		Props:     props,
		URL:       ctx.URL(),
		Version:   r.version(),
	}
	ctx.Status(status)
	return r.renderHTML(ctx, page)
}

func (r *Renderer) version() string {
	if assets := r.Assets(); assets != nil {
		return assets.Version()
	}
	return ""
}

// rootTemplateData is the data passed to the application root template
// (VIEW-02). These four placeholders are the supported contract; anything else
// in the template is the application's own choice.
type rootTemplateData struct {
	// Title is the document title.
	Title string

	// VuepherApp is the app root element with the page object embedded.
	VuepherApp template.HTML

	// VuepherAssets is the script and stylesheet tags for the current build.
	VuepherAssets template.HTML

	// VuepherVersion is the asset version, for meta tags or debugging.
	VuepherVersion string

	// VuepherComponent is the page component being rendered.
	VuepherComponent string
}

// DefaultRootTemplate is the template written by `vuepher new` (VIEW-02). It is
// kept here so that generated projects and tests agree on the placeholders.
const DefaultRootTemplate = `<!DOCTYPE html>
<html lang="{{ .VuepherLang }}">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{ .Title }}</title>
    {{ .VuepherStyleTags }}
</head>
<body>
    {{ .VuepherApp }}
    {{ .VuepherScriptTags }}
</body>
</html>
`

// TemplatePlaceholders documents the placeholders the root template must or may
// contain.
//
//	{{ .VuepherApp }}          required; the app root element with the page object
//	{{ .VuepherAssets }}       required; all script and stylesheet tags
//	{{ .VuepherStyleTags }}    optional; stylesheet tags for the document head
//	{{ .VuepherScriptTags }}   optional; script tags for the end of the body
//	{{ .VuepherVersion }}      optional; the asset version
//	{{ .VuepherComponent }}    optional; the page component name
//	{{ .Title }}               optional; the document title
//	{{ .VuepherLang }}         optional; the html lang attribute
//
// Using StyleTags/ScriptTags separately produces better first-paint behaviour
// than a single VuepherAssets tag, so generated projects use them.
var TemplatePlaceholders = []string{
	"VuepherApp",
	"VuepherAssets",
	"VuepherStyleTags",
	"VuepherScriptTags",
	"VuepherVersion",
	"VuepherComponent",
	"Title",
	"VuepherLang",
}

// templateDataWithSplits extends rootTemplateData with the split tag helpers, so
// that a template can choose the head/body arrangement without the renderer
// having to know which one it picked.
type templateDataWithSplits struct {
	rootTemplateData
	VuepherStyleTags  template.HTML
	VuepherScriptTags template.HTML
	VuepherLang       string
}

// buildTemplateData assembles the placeholder set for a page. Both the normal
// page path and the error path render through it, so a template cannot end up
// with a placeholder that only one of them populates.
func (r *Renderer) buildTemplateData(page *PageObject) (templateDataWithSplits, error) {
	r.mu.RLock()
	title := r.title
	assets := r.assets
	r.mu.RUnlock()

	encoded, err := marshalPageObject(page)
	if err != nil {
		return templateDataWithSplits{}, err
	}

	var styleTags, scriptTags template.HTML
	if assets != nil {
		styleTags = assets.StyleTags()
		scriptTags = assets.ScriptTags()
	}

	lang := r.app.Config.App.Locale
	if lang == "" {
		lang = "en"
	}

	// The JSON is already escaped for attribute context, so it is marked safe
	// here rather than being escaped a second time by the template engine (R9).
	appElement := template.HTML(`<div id="app" data-page="` + encoded + `"></div>`)

	return templateDataWithSplits{
		rootTemplateData: rootTemplateData{
			Title:            title,
			VuepherApp:       appElement,
			VuepherAssets:    template.HTML(string(styleTags) + string(scriptTags)),
			VuepherVersion:   page.Version,
			VuepherComponent: page.Component,
		},
		VuepherStyleTags:  styleTags,
		VuepherScriptTags: scriptTags,
		VuepherLang:       lang,
	}, nil
}
