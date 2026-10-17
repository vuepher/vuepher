# Project structure

The layout `vuepher new` produces. Go convention (one package per directory, no
import cycles) with names a Laravel developer already knows.

```
blog/
├── app/
│   ├── controllers/        PostController.go      HTTP actions
│   ├── models/             Post.go                struct + fluent query
│   ├── middleware/         RequireAdmin.go        custom middleware
│   ├── requests/           StorePost.go           validated input structs
│   ├── services/           (optional)             business logic
│   └── providers/          user.go                container registrations
├── bootstrap/
│   └── app.go              application assembly
├── config/                 app.go, database.go, session.go, vite.go
├── database/
│   ├── migrations/         versioned, registered schema changes
│   ├── seeders/            seeders + registry
│   └── factories/          model factories
├── routes/
│   └── web.go              route definitions
├── resources/
│   ├── views/app.html      root HTML template
│   ├── js/
│   │   ├── app.ts          frontend entry (createVuepherApp)
│   │   ├── Pages/          page components (Posts/Index.vue)
│   │   ├── Components/     reusable components
│   │   ├── Layouts/        AppLayout.vue
│   │   └── types/generated/ generated .d.ts — do not edit
│   └── css/app.css
├── public/build/           Vite output (embedded by go:embed)
├── storage/                database file, logs, uploads (git-ignored)
├── stubs/                  customised generator templates (stub:publish)
├── tests/                  application tests
├── embed.go                go:embed directive for public/build
├── main.go                 entry point
├── vuepher.json            CLI configuration
├── vite.config.ts          Vite: base '/build/', manifest, alias @
├── package.json
├── tsconfig.json
├── go.mod
└── .env
```

## The directories that carry a decision

### `app/`

One package per directory, one concern per file. A controller file carries its
own props structs (`PostsIndexProps`) so the props a page receives are declared
next to the code that sends them; `vuepher types:generate` reads those
declarations and maps them to the component.

Nothing in `app/` is discovered by convention or reflection: controllers are
referenced from `routes/web.go`, providers from `bootstrap/app.go`. A reference
that does not resolve is a compile error, and "go to definition" works.

### `bootstrap/app.go`

Where the application is assembled: configuration, middleware order, database,
guards, renderer, error pages, routes. Keeping it in one function is what lets a
test build the same application without starting a server:

```go
app, err := bootstrap.App(bootstrap.Options{EnvFile: ".env.testing"})
server := httptest.NewServer(app)
```

Middleware order matters and is commented in the file: `Recover` outermost,
`Session` before `ShareProps` (which reads flash data and the CSRF token).

### `config/`

Packages that turn the typed `.env` configuration into the components the
framework needs (`config.Database(app)`, `config.Vite(app)`). Configuration
values themselves are read once, through `vuepher.LoadConfig`, into typed
structs — no stringly-typed lookups scattered through the code.

### `database/migrations/`

Migrations are Go types with `Up` and `Down`, registered explicitly in
`registry.go`:

```go
migrator.Register("2026_01_01_000100_create_posts_table", CreatePostsTable{})
```

Nothing scans the directory. A file that is not registered does not run, which is
why `make:migration` prints the line to add. Go rather than SQL because a
migration then type-checks and runs unchanged against SQLite, PostgreSQL and
MySQL; a migration that genuinely needs SQL can call `db.Schema().Exec`.

### `resources/js/Pages/`

The component name is its path relative to this directory: `Pages/Posts/Index.vue`
is the component `"Posts/Index"`, which is the string a controller passes to
`ctx.View`. `app.ts` resolves names through `import.meta.glob`, so Vite
code-splits every page without a route table to keep in sync.

### `resources/js/types/generated/`

Generated from Go structs by `vuepher types:generate` (automatically during
`vuepher dev`). Never edit these: `types:generate --check` fails CI when they are
out of date.

Current limitations, reported as warnings rather than as silently wrong types:

- Generic instantiations such as `orm.Paginator[Post]` become `unknown` — declare
  a non-generic props struct to get a typed page.
- A type from a package that is not in the configured `types.source` list becomes
  `unknown`; add the directory to typegen sources.

### `public/build/`

Vite output: content-hashed filenames, a `manifest.json`, and pre-compressed
variants. The Go side reads the manifest to emit asset tags (VIEW-03) and serves
the files itself in production with immutable caching and ETags.

Vite's `base` must equal the path the application mounts, which is `/build/` — see
the comment in `vite.config.ts`.

### `storage/`

Runtime state: the SQLite file, logs, uploads. Presumed writable and git-ignored.
`config.EnsureDatabaseDirectory` creates the directory a SQLite DSN points at.

### `stubs/`

Empty until `vuepher stub:publish` copies the embedded templates here. Once a
stub exists, the generators prefer it over the built-in copy, so a team can
standardise what its generators produce without forking the framework.

### `vuepher.json`

CLI-side configuration: dev ports, paths the generators write to, build options.
Read by `vuepher dev`, `build`, `make:*` and the type generator, so changing a
path in one place keeps the whole toolchain pointing at it.
