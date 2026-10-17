package console

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vuepher/vuepher"
)

// runRouteList prints every registered route (HTTP-03).
//
// Routes are registered in Go code rather than declared in a file, so this can
// only run inside the application binary: the standalone CLI has no way to know
// what `routes.Web(app)` registered.
func runRouteList(ctx *Context) error {
	if len(ctx.Routes) == 0 {
		fmt.Fprintln(ctx.Stdout, "No routes are registered.")
		fmt.Fprintln(ctx.Stdout, "Register them in routes/web.go, then start the application once to confirm.")
		return nil
	}

	// Sort for a stable, diffable table: route:list output ends up in pull
	// requests and issues.
	routes := make([]vuepher.RouteInfo, len(ctx.Routes))
	copy(routes, ctx.Routes)
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})

	methodWidth, pathWidth := len("METHOD"), len("PATH")
	for _, route := range routes {
		if len(route.Method) > methodWidth {
			methodWidth = len(route.Method)
		}
		if len(route.Path) > pathWidth {
			pathWidth = len(route.Path)
		}
	}

	fmt.Fprintf(ctx.Stdout, "%-*s  %-*s  %-24s  %s\n",
		methodWidth, "METHOD", pathWidth, "PATH", "NAME", "MIDDLEWARE")
	for _, route := range routes {
		name := route.Name
		if name == "" {
			name = "-"
		}
		middleware := "-"
		if route.Middleware > 0 {
			middleware = fmt.Sprintf("%d", route.Middleware)
		}
		fmt.Fprintf(ctx.Stdout, "%-*s  %-*s  %-24s  %s\n",
			methodWidth, route.Method, pathWidth, route.Path, name, middleware)
	}

	fmt.Fprintf(ctx.Stdout, "\n%s\n", pluralRoutes(len(routes)))
	return nil
}

// pluralRoutes describes the route count, so the summary line reads naturally
// whether there is one route or many.
func pluralRoutes(count int) string {
	if count == 1 {
		return "1 route"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d routes", count)
	return b.String()
}
