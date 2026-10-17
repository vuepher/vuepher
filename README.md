# Vuepher

**Full-stack MVC framework for Go + Vue 3.** Laravel-flavoured developer
experience with Go's performance and single-binary deployment.

[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Status](https://img.shields.io/badge/status-pre--alpha-orange)](#status)
[![Protocol](https://img.shields.io/badge/view%20bridge-1.0-informational)](docs/view-bridge.md)

> **Pre-alpha, version 0.1.0.** Everything described below runs today and is
> covered by the checks in [Status](#status); the API is not frozen. Features
> that are deliberately absent are listed rather than implied.

---

## What it is

Routing, validation, authentication and data access live in Go. Controllers
render Vue components over the framework's own view bridge — conceptually the
Inertia.js model — so you get a SPA without building or maintaining a REST API,
and without keeping two sets of types in sync by hand.

```
Browser ──GET /posts──▶ Go route ──▶ Controller
                                       │
                                ctx.View("Posts/Index", props)
                                       │
       ◀── HTML + <div id="app" data-page="{…}"> ──┘   first visit
       ◀── JSON page object {component, props, url, version} ──┘   <VuepherLink>
```

Go is the only place the application's rules live. The Vue side renders what the
server sent, including validation errors, so the rules cannot drift apart.

```go
// app/controllers/PostController.go
func (c *PostController) Index(ctx *vuepher.Context) error {
	posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
	if err != nil {
		return err
	}
	return vuepher.View[PostsIndexProps](ctx, "Posts/Index", vuepher.Props{"posts": posts})
}

func (c *PostController) Store(ctx *vuepher.Context) error {
	var req struct {
		Title string `json:"title" validate:"required,min=3,max=120"`
		Body  string `json:"body" validate:"required"`
	}
	if err := ctx.Validate(&req); err != nil {
		return ctx.BackWithErrors(err) // flashed to the next page as `errors`
	}

	post := &models.Post{Title: req.Title, Body: req.Body}
	query := models.Post{}.Query()
	if err := query.Create(post); err != nil {
		return err
	}
	return ctx.RedirectRoute("post.index").With("success", "Post created.")
}
```

```vue
<!-- resources/js/Pages/Posts/Index.vue -->
<script setup lang="ts">
import { useVuepherForm } from '@vuepher/vue3'

defineProps<PageProps['Posts/Index']>()          // generated from the Go structs

const form = useVuepherForm({ title: '', body: '' })
</script>

<template>
  <form @submit.prevent="form.post('/posts')">
    <input v-model="form.data.title">
    <p v-if="form.errors.title">{{ form.errors.title }}</p>
    <button :disabled="form.processing">
      {{ form.processing ? 'Saving…' : 'Save' }}
    </button>
  </form>
</template>
```

## Requirements

| | Version | Needed for |
| --- | --- | --- |
| Go | 1.26 or newer | building anything |
| Node.js | LTS | `vuepher dev`, `vuepher build` — **never** to run a built binary |

The Go floor is a dependency floor, not a style choice: `golang.org/x/crypto` and
the struct validator both require 1.26. It is declared once, as
`vuepher.MinimumGoVersion`, and read by `vuepher doctor`, the generated `go.mod`
and this table.

## Install

```bash
go install github.com/vuepher/vuepher/cmd/vuepher@latest

# short alias, same command tree
go install github.com/vuepher/vuepher/cmd/vp@latest
```

To work on the framework itself: `git clone … && cd vuepher && make verify`.

## Quick start

```bash
vuepher new blog        # Go module + Vite + Vue 3 + TS skeleton, npm install, git init
cd blog
vuepher doctor          # checks the toolchain, ports and configuration
vuepher dev             # Go with hot reload + Vite with HMR, one terminal
```

```
app     http://localhost:8080
vite    http://localhost:5173
watching app/**/*.go routes/**/*.go config/**/*.go …
Ctrl+C to stop
```

Saving a `.go` file rebuilds and restarts the server (under a second here)
without touching the Vite process, so HMR websockets survive. Saving a `.vue`
file hot-reloads the browser.

Then a full CRUD resource:

```bash
vuepher make:model Post -m              # model + migration
vuepher make:controller PostController --resource
vuepher make:view Posts/Index
vuepher migrate && vuepher db:seed
```

Follow [`docs/getting-started.md`](docs/getting-started.md) for the walkthrough
with the code in between, or read the protocol specification in
[`docs/view-bridge.md`](docs/view-bridge.md).

## One binary

```bash
vuepher build
./build/blog-linux-amd64
```

Vite builds the frontend with content-hashed filenames and pre-compressed `.br`
and `.gz` siblings, `go:embed` packs the build and the templates into the
executable, and the framework serves them itself with immutable caching, ETags
and content-encoding negotiation.

Measured on the bundled example: **19.3 MiB** for a linux/amd64 binary, no Node
at runtime, no reverse proxy, no asset directory to deploy.

## Example application

[`examples/blog`](examples/blog) is a complete application: layout, error pages,
resource controllers, migrations, factories, seeders, pagination and a form with
server-side validation. It is generated by the same CLI and kept in the
repository so the framework is exercised by something real.

```bash
cd examples/blog
npm install && vuepher migrate && vuepher db:seed && vuepher dev
```

It builds against the working copy through a relative `replace` in its `go.mod`,
so it always tests the code in this repository rather than a released version.

## Repository layout

| Path | Purpose |
| --- | --- |
| root package (`app.go`, `context.go`, `router.go`, `route.go`, `view.go`, `page.go`, `session.go`, `validation.go`, `config.go`, `container.go`, …) | Framework core, importable as `github.com/vuepher/vuepher` |
| `orm/` | Thin, opt-in GORM wrapper: fluent generic queries, paginator, migrator, factories |
| `middleware/` | Recover, logger, request ID, session, shared props, CSRF, CORS, rate limiter |
| `auth/` | Session guard, password hashing (bcrypt/argon2), `auth` and `guest` middleware |
| `support/` | Naming conventions, string helpers, embedded stub filesystem |
| `cmd/vuepher/`, `cmd/vp/` | CLI entry points |
| `internal/cli/` | Cobra command implementations |
| `internal/generator/` | Stub rendering (`text/template`) |
| `internal/typegen/` | Go struct → TypeScript `.d.ts` generation |
| `internal/procman/`, `internal/watch/` | Process supervisor and file watcher behind `vuepher dev` |
| `stubs/` | Embedded stubs: app skeleton, controllers, models, migrations, views |
| `packages/vue3/` | npm package `@vuepher/vue3` — `createVuepherApp`, `VuepherLink`, `useVuepherForm`, `usePage` |
| `examples/blog/` | Reference application |
| `tests/` | Black-box tests: Go ↔ Vue protocol contract, database integration |
| `docs/` | Protocol specification, guides, architecture decision records |

The layout a **generated application** gets is documented in
[`docs/project-structure.md`](docs/project-structure.md).

## Status

Phase 1 of the PRD roadmap, plus the parts of phases 2 and 3 that the core
depends on. Every row marked *working* is verified by the example application,
the test suite, or both.

| Area | State |
| --- | --- |
| Router, groups, resource routes, middleware pipeline, `Context` | working |
| View bridge: `ctx.View`, HTML root, page object, `409` versioning, partial reloads | working |
| Error pages as Vue components (403/404/429/500/503), unmatched route and method | working |
| Validation: struct tags, localizable messages, error bags, old input | working |
| Session: encrypted cookie and in-memory drivers, flash data, CSRF | working |
| Typed `.env` configuration, service container | working |
| ORM: fluent generic query, paginator, soft delete, transactions, raw GORM escape hatch | working |
| Migrations (run/rollback/fresh/status), factories, seeders | working |
| Auth: session guard, password hashing (bcrypt/argon2id), `auth` + `guest` middleware | working — login/register screens are not part of v0.1 |
| CLI: `new`, `make:*`, `migrate:*`, `db:seed`, `route:list`, `types:generate`, `stub:publish`, `doctor` | working |
| CLI: `dev` (supervisor + hot reload), `build` (embed, compression, cross-compile) | working |
| npm package `@vuepher/vue3` | working |
| Auto type-gen (Go AST → `.d.ts`, per-page props types) | working, subset — generic types such as `orm.Paginator[T]` fall back to `unknown` with a warning |
| Session drivers: database, Redis | not implemented (cookie fallback, reported at boot) |
| SSR, queues/scheduler, broadcasting, `tinker` REPL, Fiber adapter | out of scope for v1.0 |

Everything the repository is checked with:

```bash
make verify   # gofmt -s, go vet, go test ./..., vue-tsc on the frontend package
```

## Design principles

- **Conventions over configuration**, but never a locked box: `*gorm.DB`, Chi,
  and `net/http` middleware are all reachable when the defaults are not enough.
- **Rules live on the server.** Validation, authentication and authorisation are
  enforced in Go, where they also protect non-browser clients.
- **Thin wrappers.** The ORM adds ergonomics; it does not reimplement GORM.
- **Errors explain themselves.** Every user-facing message names the cause and
  the fix, and points at the file involved.
- **No runtime Node.js.** Node is a development and build dependency only.
- **Decisions are written down.** Non-trivial choices get an ADR in
  [`docs/adr/`](docs/adr/), including what would make us revisit them.

## Documentation

- [`docs/getting-started.md`](docs/getting-started.md) — first project to first page
- [`docs/view-bridge.md`](docs/view-bridge.md) — the Go ↔ Vue protocol, written out explicitly
- [`docs/project-structure.md`](docs/project-structure.md) — what every directory is for
- [`docs/adr/`](docs/adr/) — architecture decision records
- [`PRD Vuepher Full-Stack MVC Framework Go + Vue 3.md`](PRD%20Vuepher%20Full-Stack%20MVC%20Framework%20Go%20+%20Vue%203.md) — the specification this implements, including which requirements are still open

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Run `make verify` before opening a pull
request; new dependencies need a one-line justification in the description.

## Security

See [`SECURITY.md`](SECURITY.md) — please report vulnerabilities privately, not
as public issues.

## License

MIT — see [`LICENSE`](LICENSE).
