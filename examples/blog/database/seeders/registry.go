// Package seeders holds the application's database seeders (DB-06).
//
// A seeder is anything implementing orm.Seeder. Create one with:
//
//	vuepher make:seeder PostSeeder
//
// Seeders should be safe to run repeatedly, so prefer Upsert or UpdateOrCreate
// over Create.
package seeders

import (
	"github.com/vuepher/vuepher/orm"
)

// Registry builds the seeder registry with every seeder the application knows
// about, in run order.
func Registry() *orm.SeederRegistry {
	registry := orm.NewSeederRegistry()

	// Seeding is split so `vuepher db:seed` can run everything and
	// `vuepher db:seed PostSeeder` can run one seeder while iterating.
	registry.Register("DatabaseSeeder", DatabaseSeeder{})
	registry.Register("PostSeeder", PostSeeder{})

	// TODO: register generated seeders here:
	//
	//	registry.Register("CommentSeeder", CommentSeeder{})

	return registry
}
