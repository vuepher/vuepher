package vuepher

import (
	"encoding/json"
	"net/http"
	"strings"
)

// PageObject is the payload the server sends for a Vue page (VIEW-01). On a
// first, non-SPA visit it is embedded in the HTML document inside the
// `data-page` attribute of the app root element. On an SPA visit it is returned
// as the JSON response body.
//
// The field names are the contract with the frontend adapter and are versioned
// by ProtocolVersion (see docs/view-bridge.md).
type PageObject struct {
	// Component is the page component name resolved by the frontend, e.g.
	// "Posts/Index".
	Component string `json:"component"`

	// Props are the values handed to the component.
	Props Props `json:"props"`

	// URL is the current request URL including query string. The client uses it
	// to keep the address bar and history in sync.
	URL string `json:"url"`

	// Version is the asset version. A client whose version differs gets a 409
	// and performs a full page load (VIEW-04).
	Version string `json:"version"`

	// DeferredProps names props that were resolved lazily on this request
	// (VIEW-06). Omitted when empty.
	DeferredProps []string `json:"deferredProps,omitempty"`

	// MergeProps names props that must be merged with, rather than replace, the
	// props currently held by the client. Omitted when empty.
	//
	// TODO(phase-2): populate from a vuepher.Merge marker type.
	MergeProps []string `json:"mergeProps,omitempty"`

	// ClearHistory tells the client to replace rather than push the history
	// entry, used by redirect-after-write flows.
	ClearHistory bool `json:"clearHistory,omitempty"`

	// EncryptHistory marks the history entry as encrypted. Reserved for a
	// post-v1.0 privacy feature.
	EncryptHistory bool `json:"encryptHistory,omitempty"`
}

// PageRequest describes how the client asked for a page. It is derived from the
// request headers by parsePageRequest.
type PageRequest struct {
	// IsPageRequest is true when the request expects a JSON page object rather
	// than a full HTML document.
	IsPageRequest bool

	// Version is the asset version the client currently holds.
	Version string

	// Component is the component the client is on right now, used to validate
	// partial reloads.
	Component string

	// OnlyData lists props to include in a partial reload. Empty means all.
	OnlyData []string

	// ExceptData lists props to exclude from a partial reload.
	ExceptData []string

	// ErrorBag names the validation error bag this request wants errors from
	// (VIEW-08).
	ErrorBag string

	// IsPartial reports whether this is a partial reload.
	IsPartial bool
}

// parsePageRequest inspects the request headers and builds a PageRequest.
func parsePageRequest(r *http.Request) PageRequest {
	pr := PageRequest{
		IsPageRequest: r.Header.Get(HeaderRequest) != "",
		Version:       r.Header.Get(HeaderVersion),
		Component:     r.Header.Get(HeaderPartialComponent),
		ErrorBag:      r.Header.Get(HeaderErrorBag),
	}
	if strings.EqualFold(r.Header.Get(HeaderPartialData), "true") {
		pr.IsPartial = true
	}
	pr.OnlyData = splitHeader(r.Header.Get(HeaderPartialOnly))
	pr.ExceptData = splitHeader(r.Header.Get(HeaderPartialExcept))
	return pr
}

// TakesComponent reports whether a partial reload was intended for the given
// component. A partial reload addressed at a different component is served as a
// normal, full visit — that happens when a redirect lands the user somewhere
// they did not ask for.
func (pr PageRequest) TakesComponent(component string) bool {
	if pr.Component == "" {
		return true
	}
	return pr.Component == component
}

// Apply narrows props according to the request's partial reload instructions.
func (pr PageRequest) Apply(props Props) Props {
	if !pr.IsPartial {
		return props
	}
	if len(pr.OnlyData) > 0 {
		return props.Only(pr.OnlyData...)
	}
	if len(pr.ExceptData) > 0 {
		return props.Except(pr.ExceptData...)
	}
	return props
}

// splitHeader splits a comma-separated header value, trimming blank entries.
func splitHeader(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// marshalPageObject serialises the object for embedding in HTML (R9).
//
// The payload is placed inside an HTML attribute, so the standard JSON encoder is
// not sufficient: HTML-unsafe characters must be escaped, or a prop value could
// close the attribute and inject markup.
func marshalPageObject(page *PageObject) (string, error) {
	buf, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	return escapeJSONForHTMLAttribute(string(buf)), nil
}

// escapeJSONForHTMLAttribute makes a JSON document safe to embed in a
// double-quoted HTML attribute (R9).
//
// The escaping is done with HTML entities rather than JSON escapes, which is the
// only form that survives the round trip through the browser: the HTML parser
// decodes entities when it builds the DOM, so `dataset.page` is the original JSON
// again and the client's JSON.parse succeeds.
//
// Emitting `\u0022` for a quote, which looks equivalent, does not work: the
// escape appears at structural positions too, where JSON has no escape sequence,
// so the document parses as HTML but the client fails with "Expected property
// name". `<` and `>` are escaped as well so that a prop containing `</script>`
// cannot terminate a surrounding tag.
//
// The delimiters are HTML-escaped to `&quot;`/`&#39;` and the ampersand first, so
// that no entity is escaped twice.
func escapeJSONForHTMLAttribute(in string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(in)
}
