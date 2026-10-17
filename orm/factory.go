package orm

import (
	"context"
	"fmt"
)

// Seeder populates the database with data (DB-06). Seeders are registered by the
// application and run by `vuepher db:seed`.
//
//	type PostSeeder struct{}
//
//	func (PostSeeder) Run(ctx context.Context, db *orm.DB) error {
//		return orm.NewFactory(20, func(i int) Post {
//			return Post{Title: fmt.Sprintf("Post %d", i)}
//		}).CreateWith(ctx, db)
//	}
type Seeder interface {
	// Run seeds the database. It receives the context so a long seed can be
	// cancelled.
	Run(context.Context, *DB) error
}

// SeederFunc adapts a function to the Seeder interface.
type SeederFunc func(context.Context, *DB) error

// Run implements Seeder.
func (fn SeederFunc) Run(ctx context.Context, db *DB) error { return fn(ctx, db) }

// SeederRegistry keeps seeders in the order they should run.
type SeederRegistry struct {
	order   []string
	seeders map[string]Seeder
}

// NewSeederRegistry builds an empty registry.
func NewSeederRegistry() *SeederRegistry {
	return &SeederRegistry{seeders: map[string]Seeder{}}
}

// Register adds a seeder under a name. Registering the same name twice replaces
// the earlier entry, which makes it easy to re-run a seed during development.
func (r *SeederRegistry) Register(name string, seeder Seeder) *SeederRegistry {
	if _, exists := r.seeders[name]; !exists {
		r.order = append(r.order, name)
	}
	r.seeders[name] = seeder
	return r
}

// Run executes the named seeders, or all of them when names is empty.
func (r *SeederRegistry) Run(ctx context.Context, db *DB, names ...string) error {
	selected := names
	if len(selected) == 0 {
		selected = r.order
	}
	for _, name := range selected {
		seeder, ok := r.seeders[name]
		if !ok {
			return fmt.Errorf(
				"orm: no seeder named %q is registered; available seeders: %v",
				name, r.order,
			)
		}
		if err := seeder.Run(ctx, db); err != nil {
			return fmt.Errorf("orm: seeder %q failed: %w", name, err)
		}
	}
	return nil
}

// Names lists the registered seeders in run order.
func (r *SeederRegistry) Names() []string {
	return append([]string{}, r.order...)
}

// ---------------------------------------------------------------------------
// Factories
// ---------------------------------------------------------------------------

// Factory builds model instances for tests and seeds (DB-06).
//
//	factory := orm.NewFactory(50, func(i int) Post {
//		return Post{Title: fmt.Sprintf("Post %d", i)}
//	})
//	posts, err := factory.Create(db)
//
// The build function receives a zero-based index so that generated values are
// unique without a counter shared across goroutines.
type Factory[T any] struct {
	count int
	build func(index int) T
}

// NewFactory builds a factory that produces count instances using build.
func NewFactory[T any](count int, build func(index int) T) *Factory[T] {
	if count < 0 {
		count = 0
	}
	return &Factory[T]{count: count, build: build}
}

// Make builds instances without touching the database.
func (f *Factory[T]) Make() []T {
	out := make([]T, 0, f.count)
	for i := 0; i < f.count; i++ {
		out = append(out, f.build(i))
	}
	return out
}

// Create builds instances and inserts them in a single batch.
func (f *Factory[T]) Create(db *DB) ([]T, error) {
	return f.CreateWith(context.Background(), db)
}

// CreateWith is Create with a context, so a seed can be cancelled.
func (f *Factory[T]) CreateWith(ctx context.Context, db *DB) ([]T, error) {
	items := f.Make()
	if len(items) == 0 {
		return items, nil
	}
	if db == nil || db.DB == nil {
		return nil, ErrNoConnection
	}
	if err := db.raw().WithContext(ctx).Create(&items).Error; err != nil {
		return nil, fmt.Errorf("orm: factory insert failed: %w", err)
	}
	return items, nil
}

// Times overrides how many instances the factory produces.
func (f *Factory[T]) Times(count int) *Factory[T] {
	if count < 0 {
		count = 0
	}
	f.count = count
	return f
}

// State applies a mutation to every built instance, for variants like
// "unpublished posts".
func (f *Factory[T]) State(mutate func(*T)) *Factory[T] {
	build := f.build
	f.build = func(index int) T {
		item := build(index)
		mutate(&item)
		return item
	}
	return f
}
