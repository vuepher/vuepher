package generator

import (
	"path"
	"strings"
	"text/template"

	"github.com/vuepher/vuepher/support"
)

// Names is the normalised set of identifiers derived from the name a user typed.
// Every generator takes its identifiers from here, so `vuepher make:model
// blog_post`, `BlogPost` and `blogPOST` all produce the same files, and the table
// name a model declares always matches the table its migration creates.
type Names struct {
	// Name is the Go type name: PostController, BlogPost.
	Name string

	// Camel is the lower-camel form: postController, blogPost.
	Camel string

	// Snake is the snake_case form: post_controller, blog_post.
	Snake string

	// Table is the database table name: posts, blog_posts.
	Table string

	// Plural is the pluralised, studly name: Posts, BlogPosts. Used for page
	// folder names.
	Plural string

	// Model is the model name with any Controller suffix removed: Post,
	// BlogPost. It is what the model-derived fields (Table, Plural, QueryType)
	// are built from.
	Model string

	// Resource is the route resource name and the model name without a
	// Controller suffix: post, blog_post.
	Resource string

	// Component is the Vue component path: Posts/Index.
	Component string

	// Package is a valid Go package name for the directory the file lands in.
	Package string

	// Title is a human-readable label: Blog post.
	Title string

	// QueryType is the constructor expression for the model's fluent query
	// builder, ready to paste: orm.Query[Post]().
	QueryType string

	// QueryResultType is the builder's *type*, which differs from the
	// constructor because the constructor is a generic function and the builder
	// is a generic type. Stubs that declare a return type or a parameter use
	// this one: *orm.Builder[Post].
	QueryResultType string

	// Migration is a timestamped migration name: 2026_10_16_101500_create_posts_table.
	Migration string
}

// NamesFor derives the identifiers for a type-oriented generator (controller,
// model, middleware, request, factory, seeder). A "Controller" suffix is stripped
// for the model-derived names, so `make:controller PostController --resource`
// pairs with the Post model.
func NamesFor(raw string) Names {
	base := normaliseInput(raw)
	studly := support.Studly(base)
	model := support.ModelName(studly)

	return Names{
		Name:            studly,
		Model:           model,
		Camel:           support.Camel(studly),
		Snake:           support.Snake(studly),
		Table:           support.TableName(model),
		Plural:          support.Plural(model),
		Resource:        support.Snake(model),
		Package:         support.PackageName(base),
		Title:           support.Title(model),
		QueryType:       queryType(model),
		QueryResultType: queryResultType(model),
	}
}

// NamesForView derives the identifiers for a view generator. `make:view
// Posts/Index` keeps the folder, so the component name matches what a controller
// passes to ctx.View.
func NamesForView(raw string) Names {
	normalised := normaliseInput(raw)
	segments := strings.Split(normalised, "/")
	for i, segment := range segments {
		segments[i] = support.Studly(segment)
	}

	component := strings.Join(segments, "/")
	leaf := segments[len(segments)-1]
	folder := ""
	if len(segments) > 1 {
		folder = segments[len(segments)-2]
	}

	// The page's own name drives props naming; the folder drives the model a
	// controller would use, which is what makes make:view able to pick a
	// matching generated props type (PRD §6.4).
	model := strings.TrimSuffix(folder, "s")

	return Names{
		Name:            leaf,
		Model:           model,
		Camel:           support.Camel(leaf),
		Snake:           support.Snake(leaf),
		Table:           support.TableName(model),
		Plural:          support.Plural(model),
		Resource:        support.Snake(model),
		Component:       component,
		Package:         support.PackageName(leaf),
		Title:           support.Title(leaf),
		QueryType:       queryType(model),
		QueryResultType: queryResultType(model),
	}
}

// NamesForMigration derives the identifiers for a migration generator, including
// a timestamp so the file sorts into the right place.
func NamesForMigration(raw string, at string) Names {
	names := NamesFor(raw)
	name := support.MigrationName(raw)
	names.Migration = at + "_" + name
	// A migration type name must be exported; CreatePostsTable is the
	// convention.
	names.Name = support.Studly(name)
	names.Package = "migrations"
	names.Model = support.ModelName(strings.TrimPrefix(name, "create_"))
	return names
}

// Model is the model a generated artifact pairs with. It is set explicitly for
// migrations, where the name describes the change rather than the model.
func (n Names) WithModel(model string) Names {
	model = support.Studly(model)
	n.Model = model
	n.Table = support.TableName(model)
	n.Plural = support.Plural(model)
	n.Resource = support.Snake(model)
	n.QueryType = queryType(model)
	n.QueryResultType = queryResultType(model)
	return n
}

// queryType builds the constructor expression for a model's query builder.
func queryType(model string) string {
	if model == "" {
		return "orm.Query[any]"
	}
	return "orm.Query[" + model + "]"
}

// queryResultType builds the builder's type expression. Go does not allow a
// generic function and a generic type to share a name, so the constructor is
// orm.Query[T]() and the type it returns is orm.Builder[T]. Stubs need both.
func queryResultType(model string) string {
	if model == "" {
		return "orm.Builder[any]"
	}
	return "orm.Builder[" + model + "]"
}

// normaliseInput cleans a user-supplied name: trailing file extensions, stray
// slashes and whitespace are removed so that `make:view Posts/Index.vue` and
// `make:view posts/index` behave the same.
func normaliseInput(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimSuffix(value, ".vue")
	value = strings.TrimSuffix(value, ".go")
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	// Strip a leading app/ or resources path, so a name pasted from a file path
	// still works.
	for _, prefix := range []string{"app/", "resources/js/Pages/", "resources/views/"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return path.Clean(value)
}

// standardFuncs are the template helpers every stub can use.
func standardFuncs() template.FuncMap {
	return template.FuncMap{
		"studly":   support.Studly,
		"camel":    support.Camel,
		"snake":    support.Snake,
		"kebab":    support.Kebab,
		"title":    support.Title,
		"plural":   support.Plural,
		"singular": support.Singular,
		"table":    support.TableName,
		"lower":    strings.ToLower,
		"upper":    strings.ToUpper,
		"trim":     strings.TrimSpace,
		"default": func(fallback, value string) string {
			if value == "" {
				return fallback
			}
			return value
		},
	}
}
