package controllers

import (
	"github.com/vuepher/vuepher"
)

// HomeController serves the landing page.
type HomeController struct {
}

// NewHomeController builds the controller.
func NewHomeController() *HomeController {
	return &HomeController{}
}

// Index renders the Home page.
//
// Note what is absent: no JSON endpoint, no API client, no serialisation step.
// The props map is handed straight to the Vue component, which is the whole
// point of the view bridge.
func (c *HomeController) Index(ctx *vuepher.Context) error {
	return ctx.View("Home", vuepher.Props{
		"greeting": "Hello from a Go controller",
		"version":  vuepher.Version,
		// Deferred props are resolved only when the client asks for them, so a
		// slow query cannot hold up the first paint (VIEW-06).
		//
		// "stats": vuepher.Defer(func() (any, error) {
		//     return reports.Summary(ctx)
		// }),
	})
}
