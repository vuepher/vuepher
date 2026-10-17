# Blog

A Vuepher application: Go on the backend, Vue 3 on the frontend, one binary for
deployment.

## Getting started

```bash
# 1. Install frontend dependencies.
npm install

# 2. Download Go modules.
go mod tidy

# 3. Create the database and the example tables.
vuepher migrate

# 4. Optional: seed an example account (demo@example.com / password) and posts.
vuepher db:seed

# 5. Start everything: Go server with hot reload, Vite with HMR.
vuepher dev
```

Then open <http://localhost:8080>.

`vuepher dev` runs both processes as children of one supervisor, so Ctrl+C stops
both and the Vite connection is unaffected by a Go rebuild.

## Common commands

| Command | What it does |
| --- | --- |
| `vuepher dev` | Go hot reload + Vite HMR in one terminal |
| `vuepher build` | Build the frontend, embed it, compile one binary |
| `vuepher make:controller PostController --resource` | Controller with the seven CRUD actions |
| `vuepher make:model Post -m` | Model plus a migration |
| `vuepher make:view Posts/Index` | Vue page component |
| `vuepher make:middleware EnsureAdmin` | Middleware |
| `vuepher make:request StorePost` | Validated request struct |
| `vuepher migrate` | Run pending migrations |
| `vuepher migrate:rollback` | Undo the last batch |
| `vuepher migrate:status` | Show what has run |
| `vuepher db:seed` | Seed the database |
| `vuepher route:list` | Every registered route |
| `vuepher types:generate` | Regenerate TypeScript types from Go structs |
| `vuepher doctor` | Check the toolchain and configuration |

Add `--help` to any command for its options.

## Project layout

| Path | Purpose |
| --- | --- |
| `app/controllers` | Request handlers |
| `app/models` | Models and query scopes |
| `app/requests` | Validation structs |
| `app/middleware` | Application middleware |
| `app/providers` | Bindings from framework contracts to models |
| `bootstrap` | Application wiring |
| `config` | Typed configuration |
| `database/migrations` | Versioned schema changes |
| `database/seeders` | Seed data |
| `database/factories` | Model factories for tests and seeds |
| `routes` | Route definitions |
| `resources/views/app.html` | Root HTML template |
| `resources/js/Pages` | Vue page components |
| `resources/js/Components` | Reusable components |
| `resources/js/types/generated` | Generated TypeScript declarations |

## How the view bridge works

A controller returns `ctx.View("Posts/Index", props)`. The framework answers
either a full HTML document with the props embedded in the `data-page` attribute,
or — for a visit that came from `<VuepherLink>` — the same props as JSON. The Vue
client swaps the page component without reloading. There is no REST API in
between.

## Documentation

- Vuepher documentation: <https://vuepher.dev>
- Root template placeholders: `resources/views/app.html`
