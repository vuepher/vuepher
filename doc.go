// Package vuepher implements the core of the Vuepher full-stack MVC framework.
//
// Vuepher pairs a Go backend with a Vue 3 view layer. Routing, validation,
// authentication and data access live in Go; controllers render Vue components
// through the "view bridge", a protocol conceptually compatible with
// Inertia.js. No separate REST API is required for a monolithic application.
//
// The package is importable as:
//
//	import "github.com/vuepher/vuepher"
//
// A controller looks like this:
//
//	func (c *PostController) Index(ctx *vuepher.Context) error {
//		posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
//		if err != nil {
//			return err
//		}
//		return ctx.View("Posts/Index", vuepher.Props{"posts": posts})
//	}
//
// The full protocol is specified in docs/view-bridge.md.
package vuepher

const (
	// Version is the framework version. It appears in the `X-Vuepher-Version`
	// response header and in generated application skeletons.
	Version = "0.1.0"

	// ProtocolVersion is the version of the Go <-> Vue page protocol. It is
	// independent of Version: the protocol only changes when the shape of the
	// page object or the headers change, which is a breaking change for the
	// frontend adapter.
	//
	// Bumping this requires a matching release of the npm package
	// @vuepher/vue3.
	ProtocolVersion = "1.0"

	// MinimumGoVersion is the Go toolchain the framework and the projects it
	// generates require, written the way a go.mod `go` directive wants it.
	//
	// It is the version this module itself declares, which is the only value that
	// cannot drift from reality: `go vet`, `go build` and every dependency agree
	// on it. The PRD originally targeted 1.22 (§7.3); the floor moved because
	// golang.org/x/crypto and the validator both declare 1.26, and pinning older
	// versions of a cryptography library to keep a lower number would be the wrong
	// trade. `vuepher doctor`, the generated go.mod and the documentation all read
	// this constant, so they cannot disagree.
	MinimumGoVersion = "1.26.0"
)

// Request and response headers used by the view bridge (VIEW-01, VIEW-04,
// VIEW-06). The names deliberately mirror the Inertia.js protocol so the
// mental model carries over, but this is Vuepher's own specification.
const (
	// HeaderRequest marks a request as an SPA visit that expects a JSON page
	// object instead of a full HTML document.
	HeaderRequest = "X-Vuepher"

	// HeaderVersion carries the asset version the client last saw. A mismatch
	// makes the server answer 409 so the client performs a full reload.
	HeaderVersion = "X-Vuepher-Version"

	// HeaderLocation is set on a 409, telling the client where to reload to.
	HeaderLocation = "X-Vuepher-Location"

	// HeaderPartialData requests a partial reload (VIEW-06).
	HeaderPartialData = "X-Vuepher-Partial-Data"

	// HeaderPartialComponent names the component the client is currently on. A
	// partial reload addressed at a different component is served in full.
	HeaderPartialComponent = "X-Vuepher-Partial-Component"

	// HeaderPartialOnly lists the props to include in a partial reload.
	HeaderPartialOnly = "X-Vuepher-Partial-Only"

	// HeaderPartialExcept lists the props to omit from a partial reload.
	HeaderPartialExcept = "X-Vuepher-Partial-Except"

	// HeaderErrorBag names the validation error bag a response belongs to
	// (VIEW-08).
	HeaderErrorBag = "X-Vuepher-Error-Bag"
)

// Content types for the page object response.
const (
	ContentTypeJSON = "application/json; charset=utf-8"
	ContentTypeHTML = "text/html; charset=utf-8"
)

// Env values recognised by Config.App.Env.
const (
	EnvLocal      = "local"
	EnvTesting    = "testing"
	EnvStaging    = "staging"
	EnvProduction = "production"
)
