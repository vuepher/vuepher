package vuepher

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// AssetOptions configure the static asset handler.
type AssetOptions struct {
	// Prefix is the URL prefix the handler is mounted under, e.g. "/build/". It
	// is stripped before looking the file up in fsys.
	Prefix string

	// MaxAge is the cache lifetime in seconds. Defaults to one year.
	MaxAge int

	// Immutable marks responses as immutable for their lifetime. Safe when
	// filenames are content-hashed, which Vite does, because a changed file gets
	// a new name rather than a stale cache.
	Immutable bool

	// Index is served for a directory request. Defaults to "index.html".
	Index string

	// Compressed serves a pre-compressed sibling (<file>.br or <file>.gz) when
	// the client accepts it. `vuepher build` writes those siblings.
	Compressed bool

	// FS is the asset filesystem. Must be non-nil.
	FS fs.FS
}

// AssetHandler serves a built frontend from an embedded filesystem (R8).
//
// Embedding the build is what makes the deployment story work: the binary is the
// whole application, with no separate static file directory to copy, mount or
// forget. Serving it well still matters, so this handler:
//
//   - sets an ETag derived from the content, so a rebuild that changes a file
//     changes its validator even though the embedded file has no modification
//     time;
//   - answers If-None-Match with 304, so repeat visits cost no body bytes;
//   - sets a long, immutable cache lifetime, which is safe because Vite's
//     filenames are content-hashed;
//   - negotiates a pre-compressed .br or .gz variant when the client supports one.
func AssetHandler(options AssetOptions) http.Handler {
	opts := options
	if opts.MaxAge == 0 {
		opts.MaxAge = 31536000 // one year
	}
	if opts.Index == "" {
		opts.Index = "index.html"
	}
	opts.Prefix = strings.Trim(opts.Prefix, "/")

	return &assetHandler{opts: opts}
}

type assetHandler struct {
	opts AssetOptions
}

func (h *assetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requested := h.lookupPath(r.URL.Path)
	if requested == "" {
		http.NotFound(w, r)
		return
	}

	// A directory request resolves to the index.
	if info, err := fs.Stat(h.opts.FS, requested); err == nil && info.IsDir() {
		requested = path.Join(requested, h.opts.Index)
	}
	if !fs.ValidPath(requested) {
		http.NotFound(w, r)
		return
	}

	// Pre-compressed variants win when the client will take them: the file on
	// disk is the same bytes, only already compressed, so this costs a lookup
	// rather than a compression pass.
	encoding, target := h.compressedVariant(r, requested)

	content, err := fs.ReadFile(h.opts.FS, target)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	sum := sha256.Sum256(content)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`

	header := w.Header()
	header.Set("ETag", etag)
	header.Set("Cache-Control", cacheControl(h.opts))
	if encoding != "" {
		header.Set("Content-Encoding", encoding)
		header.Add("Vary", "Accept-Encoding")
	}
	header.Set("Content-Type", contentTypeFor(requested))

	if match := r.Header.Get("If-None-Match"); match != "" && matchesETag(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	header.Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(content)
}

// lookupPath maps a request path onto a file in the asset filesystem.
func (h *assetHandler) lookupPath(requestPath string) string {
	trimmed := strings.TrimPrefix(requestPath, "/")
	if h.opts.Prefix != "" {
		if !strings.HasPrefix(trimmed, h.opts.Prefix) {
			// A request outside the mounted prefix is not ours to serve, but the
			// handler may be mounted at the root, so try the path as-is before
			// giving up.
			if !strings.HasPrefix(requestPath, "/") {
				return ""
			}
		} else {
			trimmed = strings.TrimPrefix(trimmed, h.opts.Prefix)
		}
	}
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return h.opts.Index
	}
	// Reject traversal outright rather than relying on fs.ValidPath.
	if strings.Contains(trimmed, "..") {
		return ""
	}
	return path.Clean(trimmed)
}

// compressedVariant returns the encoding and the file to read.
func (h *assetHandler) compressedVariant(r *http.Request, requested string) (string, string) {
	if !h.opts.Compressed {
		return "", requested
	}
	accept := r.Header.Get("Accept-Encoding")

	// Brotli first: it is smaller than gzip for the same content.
	if strings.Contains(accept, "br") {
		candidate := requested + ".br"
		if exists(h.opts.FS, candidate) {
			return "br", candidate
		}
	}
	if strings.Contains(accept, "gzip") {
		candidate := requested + ".gz"
		if exists(h.opts.FS, candidate) {
			return "gzip", candidate
		}
	}
	return "", requested
}

// cacheControl builds the Cache-Control header value.
func cacheControl(opts AssetOptions) string {
	value := "public, max-age=" + strconv.Itoa(opts.MaxAge)
	if opts.Immutable {
		value += ", immutable"
	}
	return value
}

// contentTypeFor resolves a content type, falling back to sniffing by extension.
func contentTypeFor(name string) string {
	if detected := mime.TypeByExtension(path.Ext(name)); detected != "" {
		return detected
	}
	switch path.Ext(name) {
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// matchesETag compares an If-None-Match header against our ETag, handling the
// list form and the wildcard.
func matchesETag(header, etag string) bool {
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// exists reports whether a file is present in a filesystem.
func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}
