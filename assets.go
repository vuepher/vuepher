package vuepher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
)

// AssetResolver produces the HTML tags that load the frontend bundle and reports
// the asset version used for cache busting (VIEW-03, VIEW-04).
//
// Two implementations exist: DevAssetResolver talks to the Vite dev server and
// keeps hot module replacement alive, ManifestAssetResolver reads the manifest
// Vite writes for a production build.
type AssetResolver interface {
	// Version identifies the current asset build. The client sends the version
	// it holds; a mismatch makes the server answer 409 so the client reloads.
	Version() string

	// Tags returns the <script>/<link> tags for the application entry point,
	// suitable for injection into the root template.
	Tags() template.HTML

	// StyleTags returns only the stylesheet tags, for templates that place CSS
	// in the head and scripts at the end of the body.
	StyleTags() template.HTML

	// ScriptTags returns only the script tags.
	ScriptTags() template.HTML
}

// ---------------------------------------------------------------------------
// Development: Vite dev server
// ---------------------------------------------------------------------------

// DevAssetResolver loads assets from a running Vite dev server. HMR works
// because the client connects to the same server the tags point at.
type DevAssetResolver struct {
	// Server is the dev server origin, e.g. "http://localhost:5173".
	Server string

	// Base is the URL prefix Vite serves under, e.g. "/build/". It has to match
	// the `base` in vite.config.ts, because that is where the dev server exposes
	// the entry point and the HMR client.
	Base string

	// Entry is the application entry, e.g. "resources/js/app.ts".
	Entry string

	// Styles are stylesheets Vite should serve directly in development.
	Styles []string
}

// NewDevAssetResolver builds a resolver for a running dev server.
func NewDevAssetResolver(cfg ViteConfig) *DevAssetResolver {
	server := strings.TrimSuffix(cfg.DevServer, "/")
	entry := cfg.EntryPoint
	if entry == "" {
		entry = "resources/js/app.ts"
	}
	return &DevAssetResolver{
		Server: server,
		Base:   cfg.BasePath(),
		Entry:  entry,
		Styles: []string{"resources/css/app.css"},
	}
}

// Version returns a constant in development. The dev server already disables
// caching, and a version that changes per request would force a full reload on
// every SPA visit.
func (d *DevAssetResolver) Version() string { return "dev" }

// Tags returns the Vite client plus the application entry.
func (d *DevAssetResolver) Tags() template.HTML {
	var b strings.Builder
	// The Vite client is what sets up the HMR websocket.
	fmt.Fprintf(&b, "<script type=\"module\" src=%q></script>\n", d.server("@vite/client"))
	for _, style := range d.Styles {
		fmt.Fprintf(&b, "<link rel=\"stylesheet\" href=%q>\n", d.url(style))
	}
	fmt.Fprintf(&b, "<script type=\"module\" src=%q></script>\n", d.url(d.Entry))
	return template.HTML(b.String())
}

// StyleTags returns the stylesheet tags only.
func (d *DevAssetResolver) StyleTags() template.HTML {
	var b strings.Builder
	for _, style := range d.Styles {
		fmt.Fprintf(&b, "<link rel=\"stylesheet\" href=%q>\n", d.url(style))
	}
	return template.HTML(b.String())
}

// ScriptTags returns the script tags only.
func (d *DevAssetResolver) ScriptTags() template.HTML {
	var b strings.Builder
	fmt.Fprintf(&b, "<script type=\"module\" src=%q></script>\n", d.server("@vite/client"))
	fmt.Fprintf(&b, "<script type=\"module\" src=%q></script>\n", d.url(d.Entry))
	return template.HTML(b.String())
}

// url renders an application-relative source path as a dev-server URL, keeping
// the base path Vite is configured with.
func (d *DevAssetResolver) url(asset string) string {
	return d.server(strings.TrimPrefix(asset, "/"))
}

// server joins the dev-server origin, the base path and a path inside it.
func (d *DevAssetResolver) server(path string) string {
	base := d.Base
	if base == "" {
		base = "/"
	}
	return d.Server + base + strings.TrimPrefix(path, "/")
}

// ---------------------------------------------------------------------------
// Production: Vite manifest
// ---------------------------------------------------------------------------

// ManifestEntry is one record in Vite's manifest.json.
type ManifestEntry struct {
	File           string   `json:"file"`
	Name           string   `json:"name"`
	Src            string   `json:"src"`
	IsEntry        bool     `json:"isEntry"`
	IsDynamicEntry bool     `json:"isDynamicEntry"`
	CSS            []string `json:"css"`
	Assets         []string `json:"assets"`
	Imports        []string `json:"imports"`
}

// ManifestAssetResolver reads the manifest a production build produced. Vite
// hashes filenames, so the tags are safe to cache immutably; the manifest hash
// is the asset version.
type ManifestAssetResolver struct {
	// Entries maps the entry source path to its build output.
	Entries map[string]ManifestEntry

	// BaseURL prefixes emitted asset paths, e.g. "/build/".
	BaseURL string

	// EntryPoint is the manifest key for the application entry.
	EntryPoint string

	version string
}

// ManifestFileName is the manifest location used by Vite 5 and later.
const ManifestFileName = ".vite/manifest.json"

// NewManifestAssetResolver loads and parses the manifest at manifestPath from
// fsys. baseURL is the public path the assets are served from.
//
// A missing manifest is reported with a message that says how to create one,
// because that is by far the most common first-run problem.
func NewManifestAssetResolver(fsys fs.FS, manifestPath, baseURL, entryPoint string) (*ManifestAssetResolver, error) {
	data, err := fs.ReadFile(fsys, manifestPath)
	if err != nil {
		return nil, fmt.Errorf(
			"vuepher: could not read the Vite manifest at %s: %w\n"+
				"  The frontend has not been built yet. Run `vuepher build`, or run `vuepher dev` for development.",
			manifestPath, err,
		)
	}
	var entries map[string]ManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("vuepher: the Vite manifest at %s is not valid JSON: %w", manifestPath, err)
	}
	if entryPoint == "" {
		entryPoint = "resources/js/app.ts"
	}
	sum := sha256.Sum256(data)
	return &ManifestAssetResolver{
		Entries:    entries,
		BaseURL:    strings.TrimSuffix(baseURL, "/") + "/",
		EntryPoint: entryPoint,
		version:    hex.EncodeToString(sum[:8]),
	}, nil
}

// Version returns the truncated manifest hash.
func (m *ManifestAssetResolver) Version() string { return m.version }

// Entry returns the manifest record for the application entry.
func (m *ManifestAssetResolver) Entry() (ManifestEntry, bool) {
	if entry, ok := m.Entries[m.EntryPoint]; ok {
		return entry, true
	}
	// Fall back to whichever record is marked as the entry, so a project with a
	// differently named entry file still works.
	for _, entry := range m.Entries {
		if entry.IsEntry {
			return entry, true
		}
	}
	return ManifestEntry{}, false
}

// Tags returns the stylesheet and script tags for the entry point.
func (m *ManifestAssetResolver) Tags() template.HTML {
	return template.HTML(string(m.StyleTags()) + string(m.ScriptTags()))
}

// StyleTags returns <link> tags for every stylesheet the entry pulls in.
func (m *ManifestAssetResolver) StyleTags() template.HTML {
	entry, ok := m.Entry()
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, css := range entry.CSS {
		fmt.Fprintf(&b, "<link rel=\"stylesheet\" href=%q>\n", m.BaseURL+strings.TrimPrefix(path.Clean(css), "/"))
	}
	return template.HTML(b.String())
}

// ScriptTags returns the module script for the entry point.
func (m *ManifestAssetResolver) ScriptTags() template.HTML {
	entry, ok := m.Entry()
	if !ok {
		return ""
	}
	return template.HTML(fmt.Sprintf(
		"<script type=\"module\" src=%q></script>\n",
		m.BaseURL+strings.TrimPrefix(path.Clean(entry.File), "/"),
	))
}

// NewAssetResolver picks the right resolver for the current configuration.
// DevServer wins when it is enabled, otherwise the build manifest is read.
//
// A missing manifest is not treated as a fatal error. Boot must succeed before
// the frontend has been built, because the database commands run through the same
// binary: `vuepher migrate` on a fresh clone has to work even though nothing has
// been compiled yet. Instead of failing, a MissingAssetsResolver is returned and
// the reason is reported when a page is actually rendered.
func NewAssetResolver(cfg ViteConfig, fsys fs.FS) (AssetResolver, error) {
	if cfg.UseDevServer {
		return NewDevAssetResolver(cfg), nil
	}

	// cfg.BuildDir is normally "public/build", so the built assets are served
	// from "/build/" — the same prefix vite.config.ts declares as its base.
	resolver, err := NewManifestAssetResolver(fsys, cfg.ManifestPath, cfg.BasePath(), cfg.EntryPoint)
	if err == nil {
		return resolver, nil
	}

	return &MissingAssetsResolver{Reason: err.Error()}, nil
}

// MissingAssetsResolver stands in when the production frontend has not been
// built. It renders no tags and refuses to render a page, with a message that
// says what to run.
//
// The alternative — failing at boot — would make `vuepher migrate` unusable on a
// fresh clone, which is the first command anyone runs.
type MissingAssetsResolver struct {
	// Reason explains what is missing, produced by the manifest reader so the
	// message names the path it looked for.
	Reason string
}

// Version returns an empty version: there is no build to identify.
func (m *MissingAssetsResolver) Version() string { return "" }

// Tags returns an HTML comment so the cause is visible in the page source.
func (m *MissingAssetsResolver) Tags() template.HTML {
	return template.HTML("<!-- vuepher: the frontend has not been built yet. Run `npm run build` or `vuepher build`. -->")
}

// StyleTags returns an empty string.
func (m *MissingAssetsResolver) StyleTags() template.HTML { return template.HTML(m.Tags()) }

// ScriptTags returns an empty string.
func (m *MissingAssetsResolver) ScriptTags() template.HTML { return "" }

// Missing reports that no build is present. Used by the renderer to fail a page
// request with an explanatory error instead of serving a broken document.
func (m *MissingAssetsResolver) Missing() bool { return true }
