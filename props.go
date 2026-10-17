package vuepher

// Props is the set of values handed to a Vue page component. Keys must match the
// component's declared props, and values must be JSON-serialisable — the page
// object is embedded in the HTML document as JSON on the first visit and
// returned as JSON on subsequent SPA visits.
//
// In development, a value that cannot be marshalled produces a 500 with the
// offending key named, rather than a silently truncated payload.
type Props map[string]any

// Merge returns a new Props containing p with others layered on top. Existing
// keys are overwritten by later maps; p itself is not modified.
func (p Props) Merge(others ...Props) Props {
	out := make(Props, len(p))
	for k, v := range p {
		out[k] = v
	}
	for _, other := range others {
		for k, v := range other {
			out[k] = v
		}
	}
	return out
}

// Only returns a new Props restricted to the named keys (VIEW-06). Missing keys
// are skipped rather than producing nil entries.
func (p Props) Only(keys ...string) Props {
	out := make(Props, len(keys))
	for _, k := range keys {
		if v, ok := p[k]; ok {
			out[k] = v
		}
	}
	return out
}

// Except returns a new Props with the named keys removed (VIEW-06).
func (p Props) Except(keys ...string) Props {
	drop := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}
	out := make(Props, len(p))
	for k, v := range p {
		if _, skip := drop[k]; skip {
			continue
		}
		out[k] = v
	}
	return out
}

// SortedKeys returns the prop keys in insertion-independent order. Handy for
// tests and for the `--check` mode of type generation.
func (p Props) SortedKeys() []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

// ---------------------------------------------------------------------------
// Deferred props (VIEW-06)
// ---------------------------------------------------------------------------

// Deferred is a prop whose value is resolved only when the client explicitly
// asks for it via a partial reload, or after the first paint for grouped props.
//
//	return ctx.View("Dashboard", vuepher.Props{
//		"stats": vuepher.Defer(func() (any, error) {
//			return reports.FetchStats(ctx)
//		}),
//	})
//
// TODO(phase-2): grouped deferred props — a group name lets the client request
// a batch of slow props in one follow-up visit.
type Deferred struct {
	resolve func() (any, error)
	group   string
}

// Defer marks a prop as lazily evaluated.
func Defer(fn func() (any, error)) *Deferred {
	return &Deferred{resolve: fn}
}

// DeferGroup marks a prop as lazily evaluated and assigns it to a named group.
func DeferGroup(group string, fn func() (any, error)) *Deferred {
	return &Deferred{resolve: fn, group: group}
}

// Resolve evaluates the deferred prop. It is called by the view bridge, never by
// user code.
func (d *Deferred) Resolve() (any, error) {
	if d == nil || d.resolve == nil {
		return nil, nil
	}
	return d.resolve()
}

// Group reports the deferred prop's group name, empty when ungrouped.
func (d *Deferred) Group() string {
	if d == nil {
		return ""
	}
	return d.group
}

// resolveDeferred walks the props and resolves any Deferred value. Returns the
// resolved map plus the names of props that were resolved, which the client uses
// to know which deferred props have already been loaded.
func resolveDeferred(props Props) (Props, []string, error) {
	var resolved []string
	if len(props) == 0 {
		return props, resolved, nil
	}
	out := make(Props, len(props))
	for k, v := range props {
		d, ok := v.(*Deferred)
		if !ok {
			out[k] = v
			continue
		}
		value, err := d.Resolve()
		if err != nil {
			return nil, nil, err
		}
		out[k] = value
		resolved = append(resolved, k)
	}
	sortStrings(resolved)
	return out, resolved, nil
}
