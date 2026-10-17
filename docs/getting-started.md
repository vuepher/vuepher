# Getting started

From nothing to a working CRUD page in Go + Vue. Ten minutes, one terminal, two
processes that behave like one.

## Requirements

| | Version | Needed for |
| --- | --- | --- |
| Go | ≥ 1.22 | everything |
| Node.js | LTS | development and `vuepher build` only — never at runtime |

Check both, and anything else the toolchain needs, with:

```bash
vuepher doctor
```

## 1. Create a project

```bash
vuepher new blog
cd blog
```

The generator writes the Go module, the Vite + Vue 3 + TypeScript frontend, the
application skeleton (controllers, models, routes, migrations, seeders, error
pages, layout), a `.env`, and runs `git init`. It also installs the frontend
dependencies, which is the slow step.

Everything a controller needs is registered in `bootstrap/app.go`; routes go in
`routes/web.go`.

## 2. Run it

```bash
vuepher dev
```

One command, two child processes: the Go server and the Vite dev server, with
colour-prefixed logs in one terminal.

```
app     http://localhost:8080
vite    http://localhost:5173
watching app/**/*.go routes/**/*.go ...
Ctrl+C to stop
```

- Saving a `.go` file rebuilds and restarts the Go server (sub-second here), and
  the Vite process is left alone so HMR websockets survive.
- Saving a `.vue` file hot-reloads in the browser.
- `vuepher types:generate` re-runs as Go structs change, so TypeScript props stay
  in sync.

Open <http://localhost:8080> — the page is a Vue component, rendered by a Go
controller.

## 3. Make a model and a migration

```bash
vuepher make:model Post -m
```

Two files, generated from the same name:

- `app/models/Post.go` — the struct, its `TableName`, and `Query()` which starts a
  fluent query.
- `database/migrations/<timestamp>_create_posts_table.go`.

Add your columns to the struct:

```go
type Post struct {
	vuepher.Model

	Title     string `json:"title" validate:"required,min=3,max=120"`
	Body      string `json:"body" validate:"required"`
	Published bool   `json:"published"`
}
```

The struct is the single source of truth: the column comes from the field, the
prop name the Vue component receives comes from the `json` tag, and the rule
`ctx.Validate` enforces comes from the `validate` tag.

Register the migration in `database/migrations/registry.go`, then:

```bash
vuepher migrate          # run pending migrations
vuepher migrate:status   # show what has run
vuepher migrate:rollback # undo the last batch
```

## 4. Generate the controller and the pages

```bash
vuepher make:controller PostController --resource
vuepher make:view Posts/Index
vuepher make:view Posts/Create
```

The resource controller gets all seven RESTful actions, already wired to
`models.Post` because the model exists. Register it in `routes/web.go`:

```go
app.Resource("posts", &controllers.PostController{})
```

`vuepher route:list` prints everything that is registered, with names:

```
GET     /posts              article.index   →   PostController.Index
```

## 5. Render pages from Go

```go
func (c *PostController) Index(ctx *vuepher.Context) error {
	posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
	if err != nil {
		return err
	}
	return vuepher.View[PostsIndexProps](ctx, "Posts/Index", vuepher.Props{"posts": posts})
}
```

The same props become a full HTML document on a first visit and a JSON page
object for a `<VuepherLink>` visit, so there is no REST endpoint to write, no
fetch call to hand-roll and no CORS to configure. The protocol is specified in
[view-bridge.md](view-bridge.md).

Declaring `PostsIndexProps` is optional, but naming it is what lets
`types:generate` map the component to a TypeScript interface, so the page can
write `defineProps<PageProps['Posts/Index']>()` and get the props checked.

## 6. Forms and validation

Validation runs in Go, where it also protects non-browser clients:

```go
func (c *PostController) Store(ctx *vuepher.Context) error {
	var req struct {
		Title string `json:"title" validate:"required,min=3,max=120"`
		Body  string `json:"body" validate:"required"`
	}
	if err := ctx.Validate(&req); err != nil {
		return ctx.BackWithErrors(err)
	}

	post := &models.Post{Title: req.Title, Body: req.Body}
	query := models.Post{}.Query()
	if err := query.Create(post); err != nil {
		return err
	}
	return ctx.RedirectRoute("post.index").With("success", "Post created.")
}
```

The Vue side renders whatever the server sent back:

```vue
<script setup lang="ts">
import { useVuepherForm } from '@vuepher/vue3'

const form = useVuepherForm({ title: '', body: '' })
</script>

<template>
  <form @submit.prevent="form.post('/posts')">
    <input v-model="form.data.title">
    <p v-if="form.errors.title">{{ form.errors.title }}</p>
    <button :disabled="form.processing">{{ form.processing ? 'Saving…' : 'Save' }}</button>
  </form>
</template>
```

A failed submit redirects back with the errors and the submitted input flashed onto
the session, so the form repopulates itself. `form.processing` and `form.errors`
are plain values, not refs.

## 7. Seed and inspect data

```bash
vuepher make:factory PostFactory
vuepher make:seeder PostSeeder
vuepher db:seed
```

Factories build model instances for seeds and tests:

```go
records, err := factories.NewPostFactory(20).CreateWith(ctx, db)
```

## 8. Build one binary

```bash
vuepher build
./build/blog
```

Vite builds the frontend with hashed filenames, the assets are embedded through
`go:embed`, and the result is a single static binary: no Node, no reverse proxy,
no asset directory to copy. The framework serves the embedded assets itself with
immutable cache headers and ETags.

## Where to go next

- [project-structure.md](project-structure.md) — what every directory is for
- [view-bridge.md](view-bridge.md) — the Go ↔ Vue protocol, in detail
- `vuepher stub:publish` — take ownership of the templates the generators use
- `vuepher route:list`, `vuepher types:generate`, `vuepher migrate:status` —
  inspect what the framework registered
