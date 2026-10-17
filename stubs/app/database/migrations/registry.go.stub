// Package migrations holds the application's versioned schema changes (DB-05).
//
// A migration is a Go type with an Up and a Down method. Keeping them in Go
// rather than SQL means they are type-checked and run unchanged against SQLite,
// PostgreSQL and MySQL.
//
// Create one with:
//
//	vuepher make:migration create_comments_table
//
// then add it to Registry below.
package migrations

import (
	"github.com/vuepher/vuepher/orm"
)

// Registry builds the migrator with every migration the application knows about.
//
// Registration order is the order migrations run in. The names carry a timestamp
// so ordering is explicit at the call site and cannot drift from the priority the
// files were written in.
func Registry(db *orm.DB) *orm.Migrator {
	migrator := orm.NewMigrator(db)

	// Framework-independent first migrations. Their timestamps sort before any
	// migration you generate, so a fresh install always creates users and posts
	// before anything that references them.
	migrator.Register("2026_01_01_000000_create_users_table", CreateUsersTable{})
	migrator.Register("2026_01_01_000100_create_posts_table", CreatePostsTable{})

	// TODO: register generated migrations here, in timestamp order:
	//
	//	migrator.Register("2026_05_04_101500_create_comments_table", CreateCommentsTable{})

	return migrator
}
