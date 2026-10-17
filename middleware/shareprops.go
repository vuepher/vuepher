package middleware

import (
	"github.com/vuepher/vuepher"
)

// SharePropsOptions configure the shared-props middleware.
type SharePropsOptions struct {
	// FlashKeys are the session keys lifted into the `flash` prop. Anything the
	// application flashes with a key in this list reaches the frontend; other
	// keys stay server-side.
	//
	// Defaults to success, error, warning, info and status.
	FlashKeys []string

	// ErrorBag names the validation error bag to expose. Empty means the
	// default bag (VIEW-08).
	ErrorBag string

	// Share adds extra props to every page. Values here are computed once per
	// request and merged before page props.
	Share vuepher.Props

	// DisableAuth omits the `auth` prop, for applications with their own
	// authentication wiring.
	DisableAuth bool

	// DisableCSRF omits the `csrf_token` prop.
	DisableCSRF bool

	// DisableApp omits the `app` prop, for applications that share their own
	// metadata under a different key.
	DisableApp bool
}

// defaultFlashKeys is the set of flash keys shared by default.
var defaultFlashKeys = []string{"success", "error", "warning", "info", "status"}

// ShareProps injects the props every page needs: flash messages, validation
// errors, old input, the authenticated user and the CSRF token (VIEW-05).
//
// It is the reason a controller does not have to mention authentication or
// flash data at all — the layout reads them from shared props.
//
// Register it after Session and after the authentication guard.
func ShareProps(options ...SharePropsOptions) vuepher.MiddlewareFunc {
	var opts SharePropsOptions
	if len(options) > 0 {
		opts = options[0]
	}
	flashKeys := opts.FlashKeys
	if len(flashKeys) == 0 {
		flashKeys = defaultFlashKeys
	}

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		session := ctx.Session()
		props := vuepher.Props{}

		// Flash messages, keyed so the frontend can do flash.success.
		flash := vuepher.Props{}
		for _, key := range flashKeys {
			if value := session.Get(key); value != nil {
				flash[key] = value
			}
		}
		if len(flash) > 0 {
			props["flash"] = flash
		} else {
			// Always present so the frontend does not need a null check.
			props["flash"] = vuepher.Props{}
		}

		// Validation errors from the previous request (VIEW-08). An empty bag
		// rather than null keeps `form.errors.title` safe to read in a template.
		bag := session.Errors(opts.ErrorBag)
		if bag == nil {
			bag = vuepher.ErrorBag{}
		}
		ctx.SetErrors(bag)
		props["errors"] = bag

		// Old input, so a failed form repopulates itself.
		if old := session.OldInput(); len(old) > 0 {
			props["old"] = old
		} else {
			props["old"] = map[string]any{}
		}

		// Authenticated user.
		if !opts.DisableAuth {
			var user any
			if guard := ctx.Auth(); guard != nil {
				user = guard.User()
			}
			props["auth"] = vuepher.Props{"user": user}
		}

		// CSRF token, for forms submitted with a non-GET method.
		if !opts.DisableCSRF {
			props["csrf_token"] = session.Token()
		}

		// Application metadata. The generated layout reads app.name for the
		// document title, so sending it here is what keeps every generated project
		// working without the controller passing it on every page.
		if !opts.DisableApp {
			if app := ctx.App(); app != nil && app.Config != nil {
				props["app"] = vuepher.Props{
					"name":  app.Config.App.Name,
					"env":   app.Config.App.Env,
					"debug": app.Config.App.Debug,
				}
			}
		}

		// Application-provided extras.
		for key, value := range opts.Share {
			props[key] = value
		}

		ctx.Share(props)
		return next(ctx)
	}
}
