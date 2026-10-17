package models

import (
	"github.com/vuepher/vuepher"
	"github.com/vuepher/vuepher/orm"
)

// Post is the example model. Delete it once you have added your own.
//
// The struct is the single source of truth for three things at once:
//
//	the database columns        via GORM conventions
//	the props the Vue page gets via the json tags
//	the validation rules        via the validate tags
//
// `vuepher types:generate` reads the json tags and emits a TypeScript
// declaration, so the Vue component's props are checked against this struct
// (TG-01 … TG-05).
type Post struct {
	vuepher.Model

	Title     string `json:"title" validate:"required,min=3,max=120"`
	Body      string `json:"body" validate:"required"`
	Published bool   `json:"published"`
}

// TableName pins the table name. Without it GORM would derive "posts" from the
// struct name, which is the same answer, but being explicit means renaming the
// struct cannot silently retarget the model.
func (Post) TableName() string { return "posts" }

// Query starts a fluent query:
//
//	posts, err := models.Post{}.Query().Where("published = ?", true).Latest().Paginate(ctx, 15)
//
// Go does not allow a generic function and a generic type to share a name, so the
// constructor is orm.Query[Post]() and the builder it returns is
// orm.Builder[Post].
func (m Post) Query() *orm.Builder[Post] {
	return orm.Query[Post]()
}

// Published is a reusable query scope (DB-07).
//
//	posts, err := models.Post{}.Query().Scope(models.Published).Get()
func Published(query *orm.Builder[Post]) *orm.Builder[Post] {
	return query.Where("published = ?", true)
}
